package main

import (
	"errors"
	"fmt"

	codecs "github.com/amsokol/mongo-go-driver-protobuf"
	pb "github.com/oguzhane/todo-hq/src/userservice/genproto"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/context"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

var (
	DbClient *mongo.Client
	usersdb  *mongo.Collection
	mongoCtx context.Context
)

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
	usersdb = DbClient.Database("master").Collection("users")
}

type UserServiceServer struct {
	tokenService TokenService
	// dbClient     *mongo.Client
}

func (u *UserServiceServer) Create(ctx context.Context, r *pb.User) (*pb.UserResponse, error) {
	// Generates a hashed version of our password
	hashedPass, err := bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	r.Password = string(hashedPass)
	// db, c, err := u.getDb(ctx)
	// if err != nil {
	// 	return nil, err
	// }
	// users := db.Collection("users")
	res, err := usersdb.InsertOne(mongoCtx, bson.M{"name": r.Name, "email": r.Email, "password": r.Password})
	if err != nil {
		return nil, err
	}
	return &pb.UserResponse{
		User: &pb.User{
			Id:    res.InsertedID.(primitive.ObjectID).Hex(),
			Name:  r.Name,
			Email: r.Email,
		},
	}, nil
}

func (u *UserServiceServer) Get(ctx context.Context, r *pb.User) (*pb.UserResponse, error) {

	// db, _, err := u.getDb(ctx)

	// if err != nil {
	// 	return nil, err
	// }

	// users := db.Collection("users")

	objectIDS, err := primitive.ObjectIDFromHex(r.Id)
	if err != nil {
		return nil, err
	}

	idDoc := bson.D{{"_id", objectIDS}}
	elem := &bson.D{}
	err = usersdb.FindOne(mongoCtx, idDoc).Decode(elem)

	if err != nil {
		return nil, err
	}
	m := elem.Map()

	return &pb.UserResponse{
		User: &pb.User{
			Id:    m["_id"].(primitive.ObjectID).Hex(),
			Name:  m["name"].(string),
			Email: m["email"].(string),
		},
	}, nil
}

func (u *UserServiceServer) Auth(ctx context.Context, r *pb.User) (*pb.Token, error) {
	// db, ctx, err := u.getDb(ctx)
	// if err != nil {
	// 	return nil, err
	// }

	// users := db.Collection("users")

	doc := bson.D{{"email", r.Email}}
	elem := &bson.D{}
	err := usersdb.FindOne(mongoCtx, doc).Decode(elem)

	if err != nil {
		return nil, err
	}
	m := elem.Map()

	usr := &pb.User{
		Id:       m["_id"].(primitive.ObjectID).Hex(),
		Name:     m["name"].(string),
		Email:    m["email"].(string),
		Password: m["password"].(string),
	}
	if err = bcrypt.CompareHashAndPassword([]byte(usr.Password), []byte(r.Password)); err != nil {
		return nil, err
	}

	// Compares our given password against the hashed password
	// stored in the database
	token, err := u.tokenService.Encode(usr)
	if err != nil {
		return nil, err
	}
	// res.Token = token
	return &pb.Token{
		Token: token,
	}, nil
}

func (u *UserServiceServer) ValidateToken(ctx context.Context, r *pb.Token) (*pb.Token, error) {
	// Decode token
	claims, err := u.tokenService.Decode(r.Token)
	if err != nil {
		return nil, err
	}

	log.Println(claims)

	if claims.User.Id == "" {
		return nil, errors.New("invalid user")
	}

	return &pb.Token{
		Token: r.Token,
		Valid: true,
	}, nil
}

func (*UserServiceServer) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

func (*UserServiceServer) Watch(*healthpb.HealthCheckRequest, healthpb.Health_WatchServer) error {
	return nil
}

// func (uss *UserServiceServer) getDb(c context.Context) (*mongo.Database, context.Context, error) {
// 	reg := codecs.Register(bson.NewRegistryBuilder()).Build()
// 	client, err := mongo.NewClient(options.Client().ApplyURI(dburi).SetRegistry(reg))
// 	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
// 	err = client.Connect(ctx)

// 	if err != nil {
// 		return nil, nil, err
// 	}
// 	return client.Database("master"), ctx, nil
// }
