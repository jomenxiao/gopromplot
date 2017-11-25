package main

import (
	"context"
	"flag"
	"github.com/araddon/dateparse"
	"github.com/ngaut/log"
	"github.com/siddontang/prom-plot/pkg/prom"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	pngDir     = "PngDir"
	timeFormat = "2006-01-02 15:04:05"
)

type Run struct {
	From      time.Time
	To        time.Time
	ScreenDir string
	JSONFiles []string
	PromExprs chan PpInfo
	Client    []*prom.Client
	Ctx       context.Context
	Cancel    context.CancelFunc
}

var (
	PrometheusAdress string
	From             string
	To               string
)

func init() {

	flag.StringVar(&PrometheusAdress, "prometheus_address", "http://192.168.2.188:9090", "input prometheus_address")
	flag.StringVar(&From, "prometheus_starttime", time.Now().Local().AddDate(0, 0, -3).Format(timeFormat), "input start time, default is 3 days ago")
	flag.StringVar(&To, "prometheus_endtime", time.Now().Local().Format(timeFormat), "input end time,default is now")

}

func main() {
	log.Info("init...")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	r := &Run{
		From:      dateparse.MustParse(From).Local(),
		To:        dateparse.MustParse(To).Local(),
		PromExprs: make(chan PpInfo, 10000),
		Ctx:       ctx,
		Cancel:    cancel,
	}
	for i := 0; i < getCPUNum(); i++ {
		c, err := prom.NewClient(PrometheusAdress)
		if err != nil {
			panic(err)
		}
		r.Client = append(r.Client, c)
	}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)
	go func() {
		sig := <-sc
		log.Errorf("Got signal [%d] to exit.", sig)
		r.Cancel()

	}()

	log.Info("start...")
	if err := r.PrefixWork(); err != nil {
		log.Errorf("can not finish prepare work with error %v", err)
		return
	}

	log.Info("get granfana data...")
	errG := r.GetExprs()
	if errG != nil {
		log.Errorf("can not get prometheus expr with error %v", errG)
		return
	}
	close(r.PromExprs)

	log.Info("crate images...")
	var wg sync.WaitGroup
	for i := 0; i < getCPUNum(); i++ {
		wg.Add(1)
		go func(c *prom.Client) {
			r.CreateImages(c)
			wg.Done()
		}(r.Client[i])
	}

	wg.Wait()

}
