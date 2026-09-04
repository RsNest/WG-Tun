package ident

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"proxyctl/internal/model"
)

func New() model.ID {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return model.ID(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	return model.ID(hex.EncodeToString(b))
}
