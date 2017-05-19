package e2e

import (
	"bytes"
	"context"
	"fmt"
	dockerTypes "github.com/docker/docker/api/types"
	dockerFilters "github.com/docker/docker/api/types/filters"
	docker "github.com/docker/docker/client"
	runlib "github.com/ohsu-comp-bio/funnel/cmd/run"
	"github.com/ohsu-comp-bio/funnel/cmd/server"
	"github.com/ohsu-comp-bio/funnel/config"
	"github.com/ohsu-comp-bio/funnel/logger"
	"github.com/ohsu-comp-bio/funnel/proto/tes"
	"github.com/ohsu-comp-bio/funnel/tests/testutils"
	"github.com/ohsu-comp-bio/funnel/util"
	"google.golang.org/grpc"
	"io/ioutil"
	"os"
	"text/template"
	"time"
)

var cli tes.TaskServiceClient
var log = logger.New("e2e")
var rate = time.Millisecond * 10
var dcli *docker.Client
var startTime = fmt.Sprintf("%d", time.Now().Unix())
var storageDir string
var minioKey = "AKIAIOSFODNN7EXAMPLE"
var minioSecret = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

func init() {
	logger.ForceColors()
	conf := config.DefaultConfig()
	conf = testutils.TempDirConfig(conf)
	conf = testutils.RandomPortConfig(conf)
	conf.LogLevel = "debug"
	conf.Worker.LogUpdateRate = rate
	conf.Worker.UpdateRate = rate
	conf.ScheduleRate = rate

	storageDir, _ = ioutil.TempDir("./test_tmp", "funnel-test-storage-")
	wd, _ := os.Getwd()

	// TODO need to fix the storage config so that you can't accidentally
	//      configure both S3 and Local on the same StorageConfig object,
	//      which is not valid.
	conf.Storage = append(conf.Storage,
		&config.StorageConfig{
			Local: config.LocalStorage{
				AllowedDirs: []string{storageDir, wd},
			},
		},
		&config.StorageConfig{
			S3: config.S3Storage{
				Endpoint: "localhost:9999",
				Key:      minioKey,
				Secret:   minioSecret,
			},
		},
	)

	go server.Run(conf)
	time.Sleep(time.Second)

	conn, err := grpc.Dial(conf.RPCAddress(), grpc.WithInsecure())
	if err != nil {
		panic(err)
	}

	cli = tes.NewTaskServiceClient(conn)

	var derr error
	dcli, derr = util.NewDockerClient()
	if derr != nil {
		panic(derr)
	}
}

// wait for a "destroy" event from docker for the given container ID
// TODO probably could use docker.ContainerWait()
// https://godoc.org/github.com/moby/moby/client#Client.ContainerWait
func waitForDockerDestroy(id string) {
	f := dockerFilters.NewArgs()
	f.Add("type", "container")
	f.Add("container", id)
	f.Add("event", "destroy")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	s, err := dcli.Events(ctx, dockerTypes.EventsOptions{
		Since:   string(startTime),
		Filters: f,
	})
	for {
		select {
		case e := <-err:
			panic(e)
		case <-s:
			return
		}
	}
}

// cancel a task by ID
func cancel(id string) error {
	_, err := cli.CancelTask(context.Background(), &tes.CancelTaskRequest{
		Id: id,
	})
	return err
}

// get a task by ID
func get(id string) *tes.Task {
	t, err := cli.GetTask(context.Background(), &tes.GetTaskRequest{
		Id:   id,
		View: tes.TaskView_FULL,
	})
	if err != nil {
		panic(err)
	}
	return t
}

// run a task and return it's ID
func run(s string) string {
	// Process the string as a template to allow a few helpers
	tpl := template.Must(template.New("run").Parse(s))
	var by bytes.Buffer
	data := map[string]string{
		"storage": "./" + storageDir,
	}
	if eerr := tpl.Execute(&by, data); eerr != nil {
		panic(eerr)
	}
	s = by.String()

	tasks, err := runlib.ParseString(s)
	if err != nil {
		panic(err)
	}
	if len(tasks) > 1 {
		panic("Funnel run only handles a single task (no scatter)")
	}
	log.Debug("TASK", tasks[0])
	resp, cerr := cli.CreateTask(context.Background(), tasks[0])
	if cerr != nil {
		panic(cerr)
	}
	return resp.Id
}

// wait for a task to complete
func wait(id string) *tes.Task {
	for range time.NewTicker(rate).C {
		t := get(id)
		if t.State != tes.State_QUEUED && t.State != tes.State_INITIALIZING &&
			t.State != tes.State_RUNNING {
			return t
		}
	}
	return nil
}

// wait for a task to be in the RUNNING state
func waitForRunning(id string) {
	for range time.NewTicker(rate).C {
		t := get(id)
		if t.State == tes.State_RUNNING {
			return
		}
	}
}

// wait for a task to reach the given executor index.
// 1 is the first executor.
func waitForExec(id string, i int) {
	for range time.NewTicker(rate).C {
		t := get(id)
		if len(t.Logs[0].Logs) >= i {
			return
		}
	}
}

// write a file to local storage
func writeFile(name string, content string) {
	err := ioutil.WriteFile(storageDir+"/"+name, []byte(content), os.ModePerm)
	if err != nil {
		panic(err)
	}
}

// read a file from local storage
func readFile(name string) string {
	b, err := ioutil.ReadFile(storageDir + "/" + name)
	if err != nil {
		panic(err)
	}
	return string(b)
}