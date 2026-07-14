package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	vmdockerSchema "github.com/cryptowizard0/vmdockerv2/vmdocker/schema"
	"github.com/cryptowizard0/vmdockerv2_agent/common"
	"github.com/cryptowizard0/vmdockerv2_agent/runtime"
	"github.com/gin-gonic/gin"
)

func (s *Server) runAPI(endpoint string) {
	s.engine.Use(common.CORSMiddleware())
	// api
	engine := s.engine.Group("/vmm")
	engine.POST("/health", s.health)
	engine.POST("/apply", s.apply)
	engine.POST("/checkpoint", s.checkpoint)
	engine.POST("/restore", s.restore)
	engine.POST("/spawn", s.spawn)

	// disable MPTCP
	lc := net.ListenConfig{}
	lc.SetMultipathTCP(false)

	ctx := context.Background()
	listener, err := lc.Listen(ctx, "tcp4", endpoint)
	if err != nil {
		log.Error("listen failed", "err", err)
		return
	}

	s.srv = &http.Server{
		Addr:    endpoint,
		Handler: s.engine,
	}

	if err := s.srv.Serve(listener); err != nil {
		log.Error("http Serve failed", "err", err)
	}
}

func (s *Server) closeAPI() error {
	if s.srv == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.srv.Shutdown(ctx); err != nil {
		log.Error("server forced to shutdown", "err", err)
		return fmt.Errorf("server forced to shutdown: %v", err)
	}

	log.Info("server exiting")
	return nil
}

func (s *Server) apply(c *gin.Context) {
	if s.runtime == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"msg":    "runtime is nil",
		})
		return
	}

	var req vmdockerSchema.ApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}
	result, err := s.runtime.Apply(req.From, req.Meta, req.Params)
	if err != nil {
		log.Error("apply failed", "err", err)
		msg := fmt.Sprintf("apply failed: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": msg,
		})
		return
	}
	log.Info("apply success", "result", result)
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"result": result,
	})
}

func (s *Server) spawn(c *gin.Context) {
	if s.runtime != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"msg":    "runtime is not nil",
		})
		return
	}
	var req vmdockerSchema.SpawnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	params := make(map[string]string)
	for _, tag := range req.Tags {
		params[tag.Name] = tag.Value
	}

	r, err := runtime.New(
		req.Evn,
		req.CuAddr,
		s.aoPath,
		req.Tags,
		params,
	)
	if err != nil {
		log.Error("create runtime failed", "err", err)
		msg := fmt.Sprintf("create runtime failed: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": msg,
		})
		return
	}
	s.runtime = r
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

func (s *Server) checkpoint(c *gin.Context) {
	if s.runtime == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"msg":    "runtime is nil",
		})
		return
	}

	state, err := s.runtime.Checkpoint()
	if err != nil {
		log.Error("checkpoint failed", "err", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("checkpoint failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, runtimeCheckpointResponse{
		Status: "ok",
		State:  state,
	})
}

func (s *Server) restore(c *gin.Context) {
	if s.runtime != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"msg":    "runtime is not nil",
		})
		return
	}

	var req runtimeRestoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	r, err := runtime.NewRestored(req.Env, "", s.aoPath, req.Tags, req.State)
	if err != nil {
		log.Error("restore runtime failed", "err", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("restore runtime failed: %v", err),
		})
		return
	}

	s.runtime = r
	c.JSON(http.StatusOK, runtimeRestoreResponse{
		Status: "ok",
	})
}

func (s *Server) health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := s.launcher.Ready(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"reason": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
