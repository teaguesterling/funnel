package server

import (
	"context"
	"github.com/ohsu-comp-bio/funnel/config"
	pbf "github.com/ohsu-comp-bio/funnel/proto/funnel"
	"github.com/ohsu-comp-bio/funnel/proto/tes"
	"github.com/ohsu-comp-bio/funnel/tests/testutils"
	"testing"
)

// Test a scheduled task is removed from the task queue.
func TestScheduledTaskRemovedFromQueue(t *testing.T) {

	conf := config.DefaultConfig()
	conf = testutils.TempDirConfig(conf)

	// Create database
	db, dberr := NewTaskBolt(conf)
	if dberr != nil {
		t.Fatal("Couldn't open database")
	}

	ctx := context.Background()
	task := &tes.Task{
		Id: "task-1",
		Executors: []*tes.Executor{
			{
				ImageName: "ubuntu",
				Cmd:       []string{"echo"},
			},
		},
	}
	db.CreateTask(ctx, task)

	res := db.ReadQueue(10)
	if len(res) != 1 {
		t.Fatal("Expected task in queue")
	}

	db.AssignTask(task, &pbf.Worker{
		Id: "worker-1",
	})

	res2 := db.ReadQueue(10)
	if len(res2) != 0 {
		t.Fatal("Expected task queue to be empty")
	}
}
