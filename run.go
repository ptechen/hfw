package hfw

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	//pprof
	"net/http"
	_ "net/http/pprof"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/hsyan2008/go-logger/logger"
	"github.com/hsyan2008/hfw2/common"
	"github.com/hsyan2008/hfw2/configs"
	"github.com/hsyan2008/hfw2/redis"
	"github.com/hsyan2008/hfw2/serve"
	"github.com/hsyan2008/hfw2/stack"
)

//ENVIRONMENT ..
var ENVIRONMENT string

//APPPATH 项目路径
var APPPATH = common.GetAppPath()

//APPNAME 项目名称
var APPNAME = common.GetAppName()

var PID = os.Getpid()

var Config configs.AllConfig

var DefaultRedisIns *redis.Redis

//Init 必须手动Init，方便手动设置ENVIRONMENT
func Init() {
	loadConfig()
	setLog()

	if common.IsExist("/opt/log") {
		stack.SetupStack(filepath.Join("/opt/log", APPNAME+"_stack.log"))
	} else {
		stack.SetupStack(filepath.Join(APPPATH, APPNAME+"_stack.log"))
	}
}

//setLog 初始化log写入文件
func setLog() {
	lc := Config.Logger
	logger.SetLogGoID(lc.LogGoID)

	if lc.LogFile != "" {
		logger.SetLevelStr(lc.LogLevel)
		logger.SetConsole(lc.IsConsole)
		if lc.LogType == "daily" {
			logger.SetRollingDaily(lc.LogFile)
		} else if lc.LogType == "roll" {
			logger.SetRollingFile(lc.LogFile, lc.LogMaxNum, lc.LogSize, lc.LogUnit)
		} else {
			panic("undefined logtype")
		}
	} else {
		logger.SetLevelStr("debug")
		if common.IsExist("/opt/log") {
			logger.SetRollingDaily(filepath.Join("/opt/log", APPNAME+".log"))
		} else {
			logger.SetRollingDaily(filepath.Join(APPPATH, APPNAME+".log"))
		}
	}

	logger.SetPrefix(fmt.Sprintf("Pid:%d", PID))
}

func loadConfig() {

	if common.IsExist(filepath.Join(APPPATH, "config")) {
		flag.StringVar(&ENVIRONMENT, "e", "dev", "set env, e.g dev test prod")
		flag.Parse()

		configPath := filepath.Join(APPPATH, "config", ENVIRONMENT, "config.toml")
		if common.IsExist(configPath) {
			_, err := toml.DecodeFile(configPath, &Config)
			if err != nil {
				panic(err)
			}
		}
	}

	// logger.Info(Config)

	initConfig()

	if Config.Redis.Server != "" {
		DefaultRedisIns = redis.NewRedis(Config.Redis)
	}
}

//Run start
func Run() (err error) {
	//防止被挂起，若webview
	if randPortListener != nil {
		defer os.Exit(0)
	}

	logger.Debug("Pid:", os.Getpid(), "Starting ...")
	defer logger.Debug("Pid:", os.Getpid(), "Shutdown complete!")

	logger.Debug("Start to run, Config ENVIRONMENT is", ENVIRONMENT, "APPNAME is", APPNAME, "APPPATH is", APPPATH)

	//监听信号
	go Ctx.listenSignal()

	//等待工作完成
	defer Ctx.Shutdowned()

	if randPortListener == nil {
		if Config.Server.Address == "" {
			return
		}

		Ctx.IsHTTP = true

		err = serve.Start(Config)
	} else {
		err = http.Serve(randPortListener, nil)
	}

	return
}

var randPortListener net.Listener

func RunRandPort() string {
	randPortListener, _ = net.Listen("tcp", "127.0.0.1:0")
	go func() {
		_ = Run()
	}()
	return randPortListener.Addr().String()
}

func initConfig() {
	//设置默认路由
	if Config.Route.DefaultController == "" {
		Config.Route.DefaultController = "index"
	}
	if Config.Route.DefaultAction == "" {
		Config.Route.DefaultAction = "index"
	}
	//转为绝对路径
	if !filepath.IsAbs(Config.Template.HTMLPath) {
		Config.Template.HTMLPath = filepath.Join(APPPATH, Config.Template.HTMLPath)
	}

	if Config.Server.Port != "" && !strings.Contains(Config.Server.Port, ":") {
		Config.Server.Port = ":" + Config.Server.Port
	}
	//兼容
	if Config.Server.Address == "" && Config.Server.Port != "" {
		Config.Server.Address = Config.Server.Port
	}

	certFile := Config.Server.HTTPSCertFile
	keyFile := Config.Server.HTTPSKeyFile
	if certFile != "" && keyFile != "" {
		if !filepath.IsAbs(certFile) {
			certFile = filepath.Join(APPPATH, certFile)
		}

		if !filepath.IsAbs(keyFile) {
			keyFile = filepath.Join(APPPATH, keyFile)
		}
	}

	if Config.Server.ReadTimeout == 0 {
		Config.Server.ReadTimeout = 60
	}
	if Config.Server.WriteTimeout == 0 {
		Config.Server.WriteTimeout = 60
	}
}
