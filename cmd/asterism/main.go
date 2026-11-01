package main

import (
	"flag"
	"io"
	"os"
	"strings"

	asterism "asterism/internal"
	"asterism/internal/extract"
	"asterism/internal/provider"
)

// 程序入口
func main() {
	flags := flag.NewFlagSet("asterism", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("source", "", "")
	token := flags.String("token", "", "")
	port := flags.Int("port", 8720, "")
	if flags.Parse(os.Args[1:]) != nil {
		os.Exit(2)
	}

	if err := asterism.OpenData(); err != nil {
		os.Stderr.WriteString("初始化失败：" + err.Error())
		os.Exit(1)
	}
	if err := asterism.OpenDatabase(); err != nil {
		asterism.Log("打开失败：%v", err)
		os.Exit(1)
	}
	defer asterism.CloseDatabase()

	config := asterism.Config{Token: *token, Port: *port}
	for _, name := range strings.Split(*source, ",") {
		if name = strings.TrimSpace(name); name != "" {
			config.Sources = append(config.Sources, name)
		}
	}

	if config.Port == 0 {
		if err := extract.Run(config, provider.Registry); err != nil {
			asterism.Log("运行失败：%v", err)
			os.Exit(1)
		}
		return
	}

	go extract.Daemon(config, provider.Registry)
	if err := asterism.Serve(config.Port, provider.Registry); err != nil {
		asterism.Log("服务失败：%v", err)
		os.Exit(1)
	}
}
