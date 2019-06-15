package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/golang/protobuf/ptypes"

	"github.com/golang/protobuf/ptypes/timestamp"

	pb "github.com/oguzhane/todo-hq/src/todoservice/genproto"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"cloud.google.com/go/profiler"
	"contrib.go.opencensus.io/exporter/stackdriver"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opencensus.io/exporter/jaeger"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"

	codecs "github.com/amsokol/mongo-go-driver-protobuf"
)

var (
	cat          pb.ListTodosResponse
	catalogMutex *sync.Mutex
	log          *logrus.Logger
	extraLatency time.Duration

	port = "3550"

	reloadCatalog bool
	dburi         = "mongodb://localhost:27017"
)

func init() {
	log = logrus.New()
	log.Formatter = &logrus.JSONFormatter{
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "severity",
			logrus.FieldKeyMsg:   "message",
		},
		TimestampFormat: time.RFC3339Nano,
	}
	log.Out = os.Stdout
	catalogMutex = &sync.Mutex{}
	// err := readCatalogFile(&cat)
	// if err != nil {
	// }
}

func main() {
	go initTracing()
	go initProfiling("todoservice", "1.0.0")
	flag.Parse()

	// set injected latency
	if s := os.Getenv("EXTRA_LATENCY"); s != "" {
		v, err := time.ParseDuration(s)
		if err != nil {
			log.Fatalf("failed to parse EXTRA_LATENCY (%s) as time.Duration: %+v", v, err)
		}
		extraLatency = v
		log.Infof("extra latency enabled (duration: %v)", extraLatency)
	} else {
		extraLatency = time.Duration(0)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR1, syscall.SIGUSR2)
	go func() {
		for {
			sig := <-sigs
			log.Printf("Received signal: %s", sig)
			if sig == syscall.SIGUSR1 {
				reloadCatalog = true
				log.Infof("Enable catalog reloading")
			} else {
				reloadCatalog = false
				log.Infof("Disable catalog reloading")
			}
		}
	}()

	if os.Getenv("PORT") != "" {
		port = os.Getenv("PORT")
	}
	log.Infof("starting grpc server at :%s", port)
	run(port)
	select {}
}

func run(port string) string {
	l, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatal(err)
	}
	srv := grpc.NewServer(grpc.StatsHandler(&ocgrpc.ServerHandler{}))
	svc := &todo{}
	pb.RegisterTodoServiceServer(srv, svc)

	healthpb.RegisterHealthServer(srv, svc)
	go srv.Serve(l)
	return l.Addr().String()
}

func initJaegerTracing() {
	svcAddr := os.Getenv("JAEGER_SERVICE_ADDR")
	if svcAddr == "" {
		log.Info("jaeger initialization disabled.")
		return
	}
	// Register the Jaeger exporter to be able to retrieve
	// the collected spans.
	exporter, err := jaeger.NewExporter(jaeger.Options{
		Endpoint: fmt.Sprintf("http://%s", svcAddr),
		Process: jaeger.Process{
			ServiceName: "todoservice",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	trace.RegisterExporter(exporter)
	log.Info("jaeger initialization completed.")
}

func initStats(exporter *stackdriver.Exporter) {
	view.SetReportingPeriod(60 * time.Second)
	view.RegisterExporter(exporter)
	if err := view.Register(ocgrpc.DefaultServerViews...); err != nil {
		log.Info("Error registering default server views")
	} else {
		log.Info("Registered default server views")
	}
}

func initStackdriverTracing() {
	// TODO(ahmetb) this method is duplicated in other microservices using Go
	// since they are not sharing packages.
	for i := 1; i <= 3; i++ {
		exporter, err := stackdriver.NewExporter(stackdriver.Options{})
		if err != nil {
			log.Warnf("failed to initialize Stackdriver exporter: %+v", err)
		} else {
			trace.RegisterExporter(exporter)
			trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
			log.Info("registered Stackdriver tracing")

			// Register the views to collect server stats.
			initStats(exporter)
			return
		}
		d := time.Second * 10 * time.Duration(i)
		log.Infof("sleeping %v to retry initializing Stackdriver exporter", d)
		time.Sleep(d)
	}
	log.Warn("could not initialize Stackdriver exporter after retrying, giving up")
}

func initTracing() {
	initJaegerTracing()
	initStackdriverTracing()
}

func initProfiling(service, version string) {
	// TODO(ahmetb) this method is duplicated in other microservices using Go
	// since they are not sharing packages.
	for i := 1; i <= 3; i++ {
		if err := profiler.Start(profiler.Config{
			Service:        service,
			ServiceVersion: version,
			// ProjectID must be set if not running on GCP.
			// ProjectID: "my-project",
		}); err != nil {
			log.Warnf("failed to start profiler: %+v", err)
		} else {
			log.Info("started Stackdriver profiler")
			return
		}
		d := time.Second * 10 * time.Duration(i)
		log.Infof("sleeping %v to retry initializing Stackdriver profiler", d)
		time.Sleep(d)
	}
	log.Warn("could not initialize Stackdriver profiler after retrying, giving up")
}

type todo struct{}

func (t *todo) ListTodos(context.Context, *pb.Empty) (*pb.ListTodosResponse, error) {
	time.Sleep(extraLatency)
	t1 := &pb.Todo{
		Id:          "1",
		Title:       "Title-1",
		Description: "Description-1",
		Reminder: &timestamp.Timestamp{
			Seconds: 1000,
			Nanos:   1000,
		},
	}
	t2 := &pb.Todo{
		Id:          "2",
		Title:       "Title-2",
		Description: "Description-2",
		Reminder: &timestamp.Timestamp{
			Seconds: 1000,
			Nanos:   1000,
		},
	}
	result := []*pb.Todo{
		t1,
		t2,
	}

	return &pb.ListTodosResponse{Todos: result}, nil
}

func (t *todo) GetTodo(ctx context.Context, r *pb.GetTodoRequest) (*pb.Todo, error) {
	db, ctx, err := getDb(ctx)
	if err != nil {
		return nil, err
	}
	todos := db.Collection("todos")

	objectIDS, err := primitive.ObjectIDFromHex(r.Id)
	if err != nil {
		return nil, err
	}

	idDoc := bson.D{{"_id", objectIDS}}
	elem := &bson.D{}
	err = todos.FindOne(ctx, idDoc).Decode(elem)

	if err != nil {
		return nil, err
	}
	m := elem.Map()

	ts := dataTimeToTime(m["reminder"].(primitive.DateTime))
	rem, _ := ptypes.TimestampProto(ts)

	return &pb.Todo{
		Id:          m["_id"].(primitive.ObjectID).Hex(),
		Title:       m["title"].(string),
		Description: m["description"].(string),
		Reminder:    rem,
	}, nil
}

func (t *todo) FilterTodos(ctx context.Context, r *pb.FilterTodosRequest) (*pb.FilterTodosResponse, error) {
	db, ctx, err := getDb(ctx)
	if err != nil {
		return nil, err
	}
	todos := db.Collection("todos")

	c, err := todos.Find(ctx, bson.M{"ownerId": r.OwnerId})
	if err != nil {
		return nil, err
	}
	defer c.Close(ctx)
	resp := &pb.FilterTodosResponse{
		Results: []*pb.Todo{},
	}

	for c.Next(ctx) {
		elem := &bson.D{}
		if err = c.Decode(elem); err != nil {
			return nil, err
		}
		m := elem.Map()
		// ts :=
		ts := dataTimeToTime(m["reminder"].(primitive.DateTime))
		// tt := ts.Time()

		rem, _ := ptypes.TimestampProto(ts)
		t := &pb.Todo{
			Id:          m["_id"].(primitive.ObjectID).Hex(),
			Title:       m["title"].(string),
			Description: m["description"].(string),
			Reminder:    rem,
		}
		resp.Results = append(resp.Results, t)
	}
	if err = c.Err(); err != nil {
		return nil, err
	}
	return resp, nil
}

func dataTimeToTime(dt primitive.DateTime) time.Time {
	return time.Unix(0, int64(dt)*int64(time.Millisecond))
}

func getDb(c context.Context) (*mongo.Database, context.Context, error) {
	reg := codecs.Register(bson.NewRegistryBuilder()).Build()

	client, err := mongo.NewClient(options.Client().ApplyURI(dburi).SetRegistry(reg))
	if err != nil {
		return nil, nil, err
	}
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	err = client.Connect(ctx)
	if err != nil {
		return nil, nil, err
	}
	return client.Database("master"), ctx, nil
}

func (t *todo) Create(c context.Context, r *pb.CreateRequest) (*pb.CreateResponse, error) {
	db, ctx, err := getDb(c)
	if err != nil {
		return nil, err
	}
	r.Todo.Reminder.Nanos = 0
	// x := ptypes.TimestampProto(t)
	todos := db.Collection("todos")
	res, err := todos.InsertOne(ctx, bson.M{"title": r.Todo.Title, "description": r.Todo.Description,
		"ownerId":  r.Todo.OwnerId,
		"reminder": r.Todo.Reminder,
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

func (t *todo) Update(ctx context.Context, r *pb.UpdateRequest) (*pb.UpdateResponse, error) {
	db, ctx, err := getDb(ctx)
	if err != nil {
		return nil, err
	}

	objectIDS, err := primitive.ObjectIDFromHex(r.GetId())
	if err != nil {
		return nil, err
	}
	idDoc := bson.D{{"_id", objectIDS}}
	todos := db.Collection("todos")

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
	// fmt.Println(updateDoc)
	var after options.ReturnDocument = options.After
	res := todos.FindOneAndUpdate(ctx, idDoc, bson.D{
		{"$set", updateDoc},
	}, &options.FindOneAndUpdateOptions{
		ReturnDocument: &after,
	})
	if err = res.Err(); err != nil {
		return nil, err
	}

	elem := &bson.D{}
	err = todos.FindOne(ctx, idDoc).Decode(elem)

	if err != nil {
		return nil, err
	}
	m := elem.Map()

	ts := dataTimeToTime(m["reminder"].(primitive.DateTime))
	rem, _ := ptypes.TimestampProto(ts)

	return &pb.UpdateResponse{
		Todo: &pb.Todo{
			Id:          m["_id"].(primitive.ObjectID).Hex(),
			Title:       m["title"].(string),
			Description: m["description"].(string),
			Reminder:    rem,
		},
	}, nil
}

func (*todo) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

func (*todo) Watch(*healthpb.HealthCheckRequest, healthpb.Health_WatchServer) error {
	return nil
}
