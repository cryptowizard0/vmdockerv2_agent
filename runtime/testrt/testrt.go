package testrt

import (
	"strconv"

	"github.com/cryptowizard0/vmdockerv2_agent/common"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
	goarSchema "github.com/permadao/goar/schema"
)

const (
	TestRuntimeActionPing = "Ping"
	TestRuntimeActionEcho = "Echo"
)

var log = common.NewLog("testrt")

type RuntimeTest struct{}

func NewRuntimeTest() (*RuntimeTest, error) {
	return &RuntimeTest{}, nil
}

func (r *RuntimeTest) Apply(from string, meta vmmSchema.Meta, params map[string]string) (vmmSchema.Result, error) {
	if params == nil {
		params = map[string]string{}
	}
	log.Info("apply received", "from", from, "meta", meta, "params", params)

	action := meta.Action
	if action == "" {
		action = params["Action"]
	}
	if action == "" {
		action = TestRuntimeActionEcho
	}

	data := params["Data"]
	if data == "" {
		data = meta.Data
	}
	if data == "" {
		data = "test-runtime-ok"
	}

	responseData := data
	if action == TestRuntimeActionPing {
		responseData = "Pong"
	}

	sequence := params["Reference"]
	if sequence == "" {
		sequence = strconv.FormatInt(meta.Sequence, 10)
	}

	target := from
	if target == "" {
		target = params["From"]
	}

	result := vmmSchema.Result{
		Messages: []*vmmSchema.ResMessage{
			{
				Sequence: sequence,
				Target:   target,
				Data:     responseData,
				Tags: []goarSchema.Tag{
					{Name: "Data-Protocol", Value: "ao"},
					{Name: "Variant", Value: "hymatrix0.1"},
					{Name: "Type", Value: "Message"},
					{Name: "Runtime", Value: "test"},
					{Name: "Action", Value: action},
					{Name: "Reference", Value: sequence},
				},
			},
		},
		Spawns:      []*vmmSchema.ResSpawn{},
		Assignments: nil,
		Output: map[string]interface{}{
			"runtime":  "test",
			"action":   action,
			"pid":      meta.Pid,
			"itemId":   meta.ItemId,
			"from":     from,
			"sequence": sequence,
		},
		Data: responseData,
		Cache: map[string]string{
			"runtime": "test",
			"action":  action,
		},
		Error: nil,
	}

	log.Info("apply response", "action", action, "target", target, "sequence", sequence, "data", responseData, "messages", len(result.Messages))
	return result, nil
}

func (r *RuntimeTest) Checkpoint() (string, error) {
	return "", nil
}

func (r *RuntimeTest) Restore(string) error {
	return nil
}
