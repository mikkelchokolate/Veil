package api

import "github.com/veil-panel/veil/internal/managementstate"

type ManagementStateCodec = managementstate.ManagementStateCodec

func NewManagementStateCodec() ManagementStateCodec { return managementstate.NewManagementStateCodec() }

func managementStateDecodeError(err error) error { return managementstate.DecodeError(err) }
