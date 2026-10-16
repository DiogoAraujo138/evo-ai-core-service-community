//go:build !enterprise

package main

import (
	"evo-ai-core-service/pkg/evoextensions/chordataempresa"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// installRuntimeScope is the community-build hook. The community release runs in
// single-scope mode (runtimecontext.Default()) and does not enforce tenant headers.
// The enterprise build replaces this function via wire_enterprise.go behind the
// `enterprise` build tag.
//
// Chordata: com MULTI_EMPRESA=on liga o multi-empresa próprio
// (pkg/evoextensions/chordataempresa); sem a variável é um no-op, como no upstream.
func installRuntimeScope(v1 *gin.RouterGroup, db *gorm.DB) {
	chordataempresa.Install(v1, db)
}
