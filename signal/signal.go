// 信号处理
//kill -INT pid 终止
//kill -TERM pid 重启
//需要调用Wg.Add()
//需要监听Shutdown通道
package signal

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	logger "github.com/hsyan2008/go-logger"
)

type signalContext struct {
	IsHTTP bool `json:"-"`
	//Wg 业务方调用此变量注册工作
	Wg *sync.WaitGroup `json:"-"`
	//done 业务方调用Shutdowned函数获取所有任务已经退出的通知
	done chan bool

	mu    *sync.Mutex
	doing bool

	//Shutdown 业务方手动监听此通道获知通知
	Ctx    context.Context    `json:"-"`
	Cancel context.CancelFunc `json:"-"`

	*logger.Logger
}

var scx *signalContext

func init() {
	scx = &signalContext{
		Wg:   new(sync.WaitGroup),
		done: make(chan bool),
		mu:   new(sync.Mutex),
	}
	scx.Logger = logger.NewLogger()
	scx.Logger.SetTraceID("PRIME")
	scx.Ctx, scx.Cancel = context.WithCancel(context.Background())
}

//GetSignalContext 一般用于其他包或者非http程序
func GetSignalContext() *signalContext {
	return scx
}

//gracehttp外，增加两个信号支持
func (ctx *signalContext) Listen() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)

	logger.Mixf("Exec `kill -INT %d` will graceful exit", os.Getpid())
	logger.Mixf("Exec `kill -TERM %d` will graceful restart", os.Getpid())

	s := <-c
	logger.Mix("recv signal:", s)
	go ctx.doShutdownDone()
	if ctx.IsHTTP {
		logger.Mix("Stopping http server")
		//已有第三方处理
	} else {
		logger.Mix("Stopping console server")
		switch s {
		case syscall.SIGTERM:
			execSpec := &syscall.ProcAttr{
				Env:   os.Environ(),
				Files: []uintptr{os.Stdin.Fd(), os.Stdout.Fd(), os.Stderr.Fd()},
			}
			_, _, err := syscall.StartProcess(os.Args[0], os.Args, execSpec)
			if err != nil {
				logger.Errorf("failed to forkexec: %v", err)
			}
		case syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGINT:
		}
	}
}

func (ctx *signalContext) doShutdownDone() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if ctx.doing {
		return
	}
	ctx.doing = true

	logger.Mix("doShutdownDone start.")
	defer logger.Mix("doShutdownDone done.")

	go ctx.waitDone()

	timeout := 30
	select {
	case <-time.After(time.Duration(timeout) * time.Second):
		logger.Warnf("doShutdownDone %ds timeout", timeout)
		close(ctx.done)
	case <-ctx.done:
	}
}

//通知业务方，并等待业务方结束
func (ctx *signalContext) waitDone() {
	//context包来取消，以通知业务方
	logger.Mix("signal ctx cancel")
	ctx.Cancel()
	//等待业务方完成退出
	logger.Mix("signal ctx waitgroup wait done start")
	ctx.WgWait()
	//表示全部完成
	logger.Mix("signal ctx waitgroup wait done end")
	close(ctx.done)
}

//Shutdowned 获取是否已经全部结束，暂时只有run.go里用到
func (ctx *signalContext) Shutdowned() {
	go ctx.doShutdownDone()
	<-ctx.done
}

func (ctx *signalContext) WgAdd() {
	ctx.Wg.Add(1)
}

func (ctx *signalContext) WgDone() {
	ctx.Wg.Done()
}

func (ctx *signalContext) WgWait() {
	ctx.Wg.Wait()
}
