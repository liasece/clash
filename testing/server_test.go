package test

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Dreamacro/clash/config"
	C "github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/hub"
	"github.com/Dreamacro/clash/hub/executor"
	"github.com/Dreamacro/clash/log"
	"github.com/stretchr/testify/assert"
	"go.uber.org/automaxprocs/maxprocs"
)

func startServer(configFile string, homeDir string) (*hub.Runner, error) {
	_, _ = maxprocs.Set(maxprocs.Logger(func(string, ...any) {}))

	if homeDir != "" {
		if !filepath.IsAbs(homeDir) {
			currentDir, _ := os.Getwd()
			homeDir = filepath.Join(currentDir, homeDir)
		}
		C.SetHomeDir(homeDir)
	}

	if configFile != "" {
		if !filepath.IsAbs(configFile) {
			currentDir, _ := os.Getwd()
			configFile = filepath.Join(currentDir, configFile)
		}
		C.SetConfig(configFile)
	} else {
		configFile = filepath.Join(C.Path.HomeDir(), C.Path.Config())
		C.SetConfig(configFile)
	}

	if err := config.Init(C.Path.HomeDir()); err != nil {
		log.Fatalln("Initial configuration directory error: %s", err.Error())
	}

	var options []hub.Option
	var runner *hub.Runner
	{
		var err error
		if runner, err = hub.Parse(options...); err != nil {
			log.Fatalln("Parse config error: %s", err.Error())
		}
	}

	go func() {
		defer executor.Shutdown()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
	}()

	return runner, nil
}

func waitPortOpen(host string, ports []string) bool {
	portConn := make(map[string]net.Conn)
	for i := 0; i < 3; i++ {
		for _, port := range ports {
			if portConn[port] != nil {
				continue
			}
			timeout := time.Second
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
			if err != nil {
				fmt.Println("Connecting error:", err)
				return false
			}
			if conn != nil {
				defer conn.Close()
				portConn[port] = conn
			}
		}
		pass := true
		for _, port := range ports {
			if portConn[port] == nil {
				pass = false
			}
		}
		if pass {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

func runBasicConnTest(t *testing.T, runner *hub.Runner) {
	urlList := []string{
		"https://google.com",
		"https://www.google.com",
		"https://www.google.com.hk",
		"https://www.youtube.com",
		"http://www.gstatic.com/generate_204",
		"https://baidu.com",
	}

	//creating the proxyURL
	proxyStr := "http://localhost:" + fmt.Sprint(runner.Config.General.Port)
	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		t.Error(err)
		return
	}
	log.Warnln("proxyURL: %s", proxyURL)

	for i := 0; i < 30; i++ {

		//creating the URL to be loaded through the proxy
		urlStr := urlList[i%len(urlList)]
		url, err := url.Parse(urlStr)
		if err != nil {
			t.Error(err, i)
			return
		}

		//adding the proxy settings to the Transport object
		transport := &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}

		//adding the Transport object to the http Client
		client := &http.Client{
			Transport: transport,
		}

		//generating the HTTP GET request
		request, err := http.NewRequest("GET", url.String(), nil)
		if err != nil {
			t.Error(err, i)
			return
		}

		//calling the URL
		response, err := client.Do(request)
		if err != nil {
			t.Error(err, i)
			return
		}

		//getting the response
		data, err := ioutil.ReadAll(response.Body)
		if err != nil {
			t.Error(err, i)
			return
		}
		_ = data
		//printing the response
		// log.Infoln(string(data))

		var sleepTime time.Duration
		// sleepTime = time.Millisecond * 300
		sleepTime = time.Second * time.Duration(rand.Intn(i/10*10))
		log.Infoln("sleepTime: %s", sleepTime)
		time.Sleep(sleepTime)
	}
}

func TestClash_All(t *testing.T) {
	cfgPath := os.Getenv("TESTING_CLASH_CONFIG")
	homeDir := os.Getenv("TESTING_CLASH_HOME_DIR")
	if cfgPath == "" {
		t.Skip("no config")
	}
	runner, err := startServer(cfgPath, homeDir)
	assert.NoError(t, err)
	ok := waitPortOpen("", []string{fmt.Sprint(runner.Config.General.Port), fmt.Sprint(runner.Config.General.RedirPort)})
	assert.Equal(t, true, ok)

	time.Sleep(time.Second * 1)

	runBasicConnTest(t, runner)
}
