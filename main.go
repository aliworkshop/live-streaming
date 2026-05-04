package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aliworkshop/configer"

	"github.com/aliworkshop/live-streaming/app"
)

func main() {
	a := app.New(loadConfig(os.Getenv("env")))
	a.Init()
	a.InitServices()
	a.InitModules()
	a.Start()
}

func loadConfig(env string) configer.Registry {
	if env == "" {
		env = "local"
	}
	path, err := filepath.Abs(fmt.Sprintf("cmd/config/config-%s.yaml", env))
	if err != nil {
		panic("cannot resolve config path: " + err.Error())
	}
	f, err := os.Open(path)
	if err != nil {
		panic("cannot open config: " + err.Error())
	}
	defer f.Close()
	r := configer.New()
	r.SetConfigType("yaml")
	if err = r.ReadConfig(f); err != nil {
		panic("cannot read config: " + err.Error())
	}
	return r
}
