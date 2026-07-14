package server

import (
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
	goarSchema "github.com/permadao/goar/schema"
)

type runtimeCheckpointResponse struct {
	Status string `json:"status"`
	State  string `json:"state"`
}

type runtimeRestoreRequest struct {
	Env   vmmSchema.Env    `json:"env"`
	Tags  []goarSchema.Tag `json:"tags"`
	State string           `json:"state"`
}

type runtimeRestoreResponse struct {
	Status string `json:"status"`
}
