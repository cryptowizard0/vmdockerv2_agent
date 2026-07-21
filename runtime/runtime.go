package runtime

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/cryptowizard0/vmdockerv2_agent/common"
	"github.com/cryptowizard0/vmdockerv2_agent/runtime/claudecode"
	"github.com/cryptowizard0/vmdockerv2_agent/runtime/hermes"
	"github.com/cryptowizard0/vmdockerv2_agent/runtime/openclaw"
	"github.com/cryptowizard0/vmdockerv2_agent/runtime/schema"
	"github.com/cryptowizard0/vmdockerv2_agent/runtime/telegramcustomer"
	"github.com/cryptowizard0/vmdockerv2_agent/runtime/testrt"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
	goarSchema "github.com/permadao/goar/schema"
)

var log = common.NewLog("runtime")

const (
	RuntimeTypeTest             = "test"
	RuntimeTypeOpenclaw         = "openclaw"
	RuntimeTypeClaude           = "claude"
	RuntimeTypeHermes           = "hermes"
	RuntimeTypeTelegramCustomer = "telegramcustomer"
)

type Runtime struct {
	vm schema.IRuntime
}

func New(env vmmSchema.Env, nodeAddr, aoDir string, tags []goarSchema.Tag, spawnParams map[string]string) (*Runtime, error) {
	return newRuntime(env, nodeAddr, aoDir, tags, spawnParams, "", false)
}

func NewRestored(env vmmSchema.Env, nodeAddr, aoDir string, tags []goarSchema.Tag, state string) (*Runtime, error) {
	return newRuntime(env, nodeAddr, aoDir, tags, tagsToParams(tags), state, true)
}

func newRuntime(env vmmSchema.Env, nodeAddr, aoDir string, tags []goarSchema.Tag, spawnParams map[string]string, state string, restore bool) (*Runtime, error) {
	_ = nodeAddr
	_ = aoDir

	var vm schema.IRuntime
	var err error

	runtimeType := RuntimeTypeTest
	if envType := os.Getenv("RUNTIME_TYPE"); envType != "" {
		runtimeType = envType
	}
	log.Info("runtime type selected", "type", runtimeType)

	switch runtimeType {
	case RuntimeTypeTest:
		vm, err = testrt.NewRuntimeTest()
	case RuntimeTypeOpenclaw:
		if restore {
			vm, err = openclaw.NewRestored(state)
		} else {
			vm, err = openclaw.NewWithParams(spawnParams)
		}
	case RuntimeTypeClaude:
		if restore {
			vm, err = claudecode.NewRestored(state, spawnParams)
		} else {
			vm, err = claudecode.NewWithParams(spawnParams)
		}
	case RuntimeTypeHermes:
		if restore {
			vm, err = hermes.NewRestored(state, spawnParams)
		} else {
			vm, err = hermes.NewWithParams(spawnParams)
		}
	case RuntimeTypeTelegramCustomer:
		if restore {
			vm, err = telegramcustomer.NewRestored(state, spawnParams)
		} else {
			vm, err = telegramcustomer.NewWithParams(spawnParams)
		}
	default:
		return nil, fmt.Errorf("runtime type not supported: %s", runtimeType)
	}

	if err != nil {
		return nil, err
	}

	return &Runtime{vm: vm}, nil
}

func tagsToParams(tags []goarSchema.Tag) map[string]string {
	params := make(map[string]string, len(tags))
	for _, tag := range tags {
		params[tag.Name] = tag.Value
	}
	return params
}

func (r *Runtime) Apply(from string, meta vmmSchema.Meta, params map[string]string) (string, error) {
	response, err := r.vm.Apply(from, meta, params)
	if err != nil {
		return "", fmt.Errorf("runtime apply failed: %w", err)
	}
	outboxJson, err := json.Marshal(response)
	if err != nil {
		log.Error("marshal outbox failed", "err", err)
		return "", err
	}
	return string(outboxJson), nil
}

func (r *Runtime) Checkpoint() (string, error) {
	if r == nil || r.vm == nil {
		return "", fmt.Errorf("runtime is nil")
	}
	return r.vm.Checkpoint()
}

func (r *Runtime) Restore(data string) error {
	if r == nil || r.vm == nil {
		return fmt.Errorf("runtime is nil")
	}
	return r.vm.Restore(data)
}
