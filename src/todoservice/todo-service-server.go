package main

import (
	"fmt"
	"time"

	codecs "github.com/amsokol/mongo-go-driver-protobuf"
	"github.com/golang/protobuf/ptypes"
	pb "github.com/oguzhane/todo-hq/src/todoservice/genproto"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/net/context"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

var (
	DbClient *mongo.Client
	todosdb  *mongo.Collection
	mongoCtx context.Context
)

type TodoServiceServer struct{}

func InitDb(dburi string) {
	// non-nil empty context
	mongoCtx = context.Background()
	reg := codecs.Register(bson.NewRegistryBuilder()).Build()
	db, err := mongo.NewClient(options.Client().ApplyURI(dburi).SetRegistry(reg))
	if err != nil {
		log.Fatal(err)
	}
	DbClient = db
	DbClient.Connect(mongoCtx)
	err = DbClient.Ping(mongoCtx, nil)
	if err != nil {
		log.Fatalf("Could not connect to MongoDB: %v\n", err)
	} else {
		fmt.Println("Connected to Mongodb")
	}
	todosdb = DbClient.Database("master").Collection("todos")
}

func (t *TodoServiceServer) GetTodo(ctx context.Context, r *pb.GetTodoRequest) (*pb.Todo, error) {
	objectIDS, err := primitive.ObjectIDFromHex(r.Id)
	if err != nil {
		return nil, err
	}

	idDoc := bson.D{{"_id", objectIDS}}
	elem := &bson.D{}
	err = todosdb.FindOne(mongoCtx, idDoc).Decode(elem)

	if err != nil {
		return nil, err
	}
	m := elem.Map()

	ts := dataTimeToTime(m["reminder"].(primitive.DateTime))
	rem, _ := ptypes.TimestampProto(ts)
	ts = dataTimeToTime(m["updatedAt"].(primitive.DateTime))
	updatedAt, _ := ptypes.TimestampProto(ts)
	return &pb.Todo{
		Id:          m["_id"].(primitive.ObjectID).Hex(),
		Title:       m["title"].(string),
		Description: m["description"].(string),
		Reminder:    rem,
		UpdatedAt:   updatedAt,
		Completed:   m["completed"].(bool),
	}, nil
}

func (t *TodoServiceServer) FilterTodos(ctx context.Context, r *pb.FilterTodosRequest) (*pb.FilterTodosResponse, error) {
	filterBson := bson.M{"ownerId": r.OwnerId}
	if r.GetHasCompleted() {
		filterBson["completed"] = r.GetCompletedValue()
	}

	queries := []string{
		"$eq", "$gt", "$gte",
		"$lt", "$lte", "$ne"}
	for _, q := range queries {
		if q == r.GetReminderQueryOpValue() {
			filterBson["reminder"] = bson.M{q: r.GetReminderQueryValue()}
			break
		}
	}

	c, err := todosdb.Find(mongoCtx, filterBson)
	if err != nil {
		return nil, err
	}
	defer c.Close(mongoCtx)
	resp := &pb.FilterTodosResponse{
		Results: []*pb.Todo{},
	}

	for c.Next(mongoCtx) {
		elem := &bson.D{}
		if err = c.Decode(elem); err != nil {
			return nil, err
		}
		m := elem.Map()
		// ts :=
		ts := dataTimeToTime(m["reminder"].(primitive.DateTime))
		rem, _ := ptypes.TimestampProto(ts)
		ts = dataTimeToTime(m["updatedAt"].(primitive.DateTime))
		updatedAt, _ := ptypes.TimestampProto(ts)
		t := &pb.Todo{
			Id:          m["_id"].(primitive.ObjectID).Hex(),
			Title:       m["title"].(string),
			Description: m["description"].(string),
			Reminder:    rem,
			UpdatedAt:   updatedAt,
			Completed:   m["completed"].(bool),
			OwnerId:     m["ownerId"].(string),
		}
		resp.Results = append(resp.Results, t)
	}
	if err = c.Err(); err != nil {
		return nil, err
	}
	return resp, nil
}

func (t *TodoServiceServer) Create(c context.Context, r *pb.CreateRequest) (*pb.CreateResponse, error) {
	r.Todo.Reminder.Nanos = 0
	// x := ptypes.TimestampProto(t)
	res, err := todosdb.InsertOne(mongoCtx, bson.M{"title": r.Todo.Title, "description": r.Todo.Description,
		"ownerId":   r.Todo.OwnerId,
		"reminder":  r.Todo.Reminder,
		"updatedAt": time.Now(),
		// "reminder": primitive.Timestamp{
		// 	T: r.Todo.Reminder.Seconds,
		// 	I: r.Todo.Reminder.Nanos,
		// },
		// "reminder": bson.M{
		// 	"seconds": r.Todo.Reminder.Seconds,
		// 	"nanos":   r.Todo.Reminder.Nanos,
		// }
	})
	return &pb.CreateResponse{
		Id: res.InsertedID.(primitive.ObjectID).Hex(),
	}, err
}

func (t *TodoServiceServer) Update(ctx context.Context, r *pb.UpdateRequest) (*pb.UpdateResponse, error) {
	objectIDS, err := primitive.ObjectIDFromHex(r.GetId())
	if err != nil {
		return nil, err
	}
	idDoc := bson.D{{"_id", objectIDS}}

	updateDoc := bson.M{}
	updateDoc["_id"] = objectIDS
	// fmt.Println(r)
	if r.GetHasTitle() {
		updateDoc["title"] = r.GetTitleValue()
	}
	if r.GetHasDescription() {
		updateDoc["description"] = r.GetDescriptionValue()
	}
	if r.GetHasReminder() {
		updateDoc["reminder"] = r.GetReminderValue()
	}
	if r.GetHasCompleted() {
		updateDoc["completed"] = r.GetCompletedValue()
	}
	// fmt.Println(updateDoc)
	var after options.ReturnDocument = options.After
	res := todosdb.FindOneAndUpdate(mongoCtx, idDoc, bson.D{
		{"$set", updateDoc},
	}, &options.FindOneAndUpdateOptions{
		ReturnDocument: &after,
	})
	if err = res.Err(); err != nil {
		return nil, err
	}

	elem := &bson.D{}
	err = res.Decode(elem)

	if err != nil {
		return nil, err
	}
	m := elem.Map()

	ts := dataTimeToTime(m["reminder"].(primitive.DateTime))
	rem, _ := ptypes.TimestampProto(ts)
	ts = dataTimeToTime(m["updatedAt"].(primitive.DateTime))
	updatedAt, _ := ptypes.TimestampProto(ts)

	return &pb.UpdateResponse{
		Todo: &pb.Todo{
			Id:          m["_id"].(primitive.ObjectID).Hex(),
			Title:       m["title"].(string),
			Description: m["description"].(string),
			Reminder:    rem,
			UpdatedAt:   updatedAt,
			Completed:   m["completed"].(bool),
		},
	}, nil
}

func (*TodoServiceServer) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

func (*TodoServiceServer) Watch(*healthpb.HealthCheckRequest, healthpb.Health_WatchServer) error {
	return nil
}

func dataTimeToTime(dt primitive.DateTime) time.Time {
	return time.Unix(0, int64(dt)*int64(time.Millisecond))
}
