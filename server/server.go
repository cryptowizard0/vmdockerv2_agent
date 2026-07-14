package server

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cryptowizard0/vmdockerv2_agent/common"
	"github.com/cryptowizard0/vmdockerv2_agent/runtime"
	"github.com/cryptowizard0/vmdockerv2_agent/supervisor"
	"github.com/gin-gonic/gin"
)

var log = common.NewLog("server")

type Server struct {
	engine   *gin.Engine
	port     int
	srv      *http.Server
	launcher runtime.Launcher

	sup           *supervisor.Supervisor
	startHookPath string
	command       []string

	runtime *runtime.Runtime
	aoPath  string
	// outgoingChan chan nodeSchema.Outgoing // used to send messages to Cu
}

func New(port int, command []string) *Server {
	engine := gin.Default()
	return &Server{
		engine:   engine,
		port:     port,
		launcher: runtime.LauncherFor(runtime.CurrentRuntimeType()),
		// outgoingChan: make(chan nodeSchema.Outgoing),
		command:       command,
		aoPath:        getEnvOrDefault("AO_PATH", "./ao/2.0.1"),
		startHookPath: getEnvOrDefault("VMDOCKER_USER_STARTUP_HOOK", "/usr/local/lib/vmdocker-agent/user-startup.sh"),
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// bootRuntime prepares the engine's environment via the launcher, exports it
// into the adapter's own process environment, then spawns the user startup
// hook (start.sh) under the supervisor and starts the reap loop.
func (s *Server) bootRuntime() error {
	env, err := s.launcher.Prepare()
	if err != nil {
		return fmt.Errorf("runtime prepare: %w", err)
	}
	for _, kv := range env {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			if err := os.Setenv(parts[0], parts[1]); err != nil {
				return fmt.Errorf("export env %s: %w", parts[0], err)
			}
		}
	}

	logPath := getEnvOrDefault("VMDOCKER_USER_STARTUP_LOG", "/tmp/vmdocker-user-startup.log")
	s.sup = supervisor.New(s.command, s.startHookPath, logPath)

	sigchld := make(chan os.Signal, 1)
	signal.Notify(sigchld, syscall.SIGCHLD)
	go s.sup.ReapLoop(sigchld)

	if err := s.sup.Start(); err != nil {
		return fmt.Errorf("start user startup hook: %w", err)
	}
	return nil
}

func (s *Server) Run() error {
	log.Info("server running", "port", s.port)

	// create context
	// ctx, cancel := context.WithCancel(context.Background())
	// defer cancel()

	if err := s.bootRuntime(); err != nil {
		return fmt.Errorf("boot runtime: %w", err)
	}

	// start api
	endpoint := fmt.Sprintf(":%d", s.port)
	go s.runAPI(endpoint)

	// handle message from channel, just print it
	// go func() {
	// 	for {
	// 		select {
	// 		case msg := <-s.outgoingChan:
	// 			log.Info("received message", "msg", msg)
	// 		case <-ctx.Done():
	// 			return
	// 		}
	// 	}
	// }()

	// wait for signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// close channel
	// close(s.outgoingChan)

	if s.sup != nil {
		if err := s.sup.Forward(syscall.SIGTERM); err != nil {
			log.Error("forward SIGTERM to engine failed", "err", err)
		}
	}

	return s.closeAPI()
}

func (s *Server) Close() error {
	return s.closeAPI()
}
