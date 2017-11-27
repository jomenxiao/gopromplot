package main

import (
	"encoding/json"
	"fmt"
	"github.com/juju/errors"
	"github.com/ngaut/log"
	"github.com/prometheus/common/model"
	"github.com/siddontang/prom-plot/pkg/plot"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var loadJSON = []string{
	"https://raw.githubusercontent.com/pingcap/tidb-ansible/master/scripts/tidb.json",
	"https://raw.githubusercontent.com/pingcap/tidb-ansible/master/scripts/tikv.json",
	"https://raw.githubusercontent.com/pingcap/tidb-ansible/master/scripts/pd.json",
}

const (
	DefaultVaule = 0
)

type PpInfo struct {
	Expr string `json:"expr"`
	Name string
	Step int64 `json:"step"`
}

//FixErrorValue fix number is NaN
func (r *Run) FixErrorValue(m model.Matrix) model.Matrix {
	for i, subM := range m {
		for subI, subV := range subM.Values {
			if math.IsNaN(float64(subV.Value)) {
				m[i].Values[subI].Value = model.SampleValue(DefaultVaule)
			}
		}
	}

	return m
}

//JSONData handle json data
func (r *Run) JSONData(sourceData interface{}, title string) {
	switch s := sourceData.(type) {
	case map[string]interface{}:

		if _, ok := s["title"]; ok {
			title = s["title"].(string)
		}

		if _, ok := s["panels"]; ok {
			r.JSONData(s["panels"], title)
		}

		if _, ok := s["targets"]; ok {
			r.JSONData(s["targets"], title)
		}

		if _, ok := s["expr"]; ok {
			name := strings.Join([]string{strings.Replace(title, " ", "_", -1), s["refId"].(string)}, "_")
			r.PromExprs <- PpInfo{
				Expr: s["expr"].(string),
				Name: name,
				Step: int64(s["step"].(float64)),
			}
		}

		if _, ok := s["rows"]; ok {
			r.JSONData(s["rows"], title)
		}

	case []interface{}:
		for _, a := range s {
			r.JSONData(a, title)
		}

	}

}

//GetExprs get json info
func (r *Run) GetExprs() error {
	if Query != "" && Name != "" {
		r.PromExprs <- PpInfo{
			Expr: Query,
			Name: Name,
			Step: Step,
		}
		return nil
	}
	loopFiles := r.JSONFiles
	if len(loopFiles) == 0 {
		loopFiles = loadJSON
	}

	for _, f := range loopFiles {
		b, err := GetJSON(f)
		if err != nil {
			return err
		}
		var iface interface{}
		errJ := json.Unmarshal(b, &iface)
		if errJ != nil {
			return errJ
		}
		r.JSONData(iface, "")
	}

	return nil
}

//PrefixWork prefixWork
func (r *Run) PrefixWork() error {
	workDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return err
	}

	r.ScreenDir = filepath.Join(workDir, pngDir)
	if _, err := os.Stat(r.ScreenDir); os.IsNotExist(err) {
		return os.Mkdir(r.ScreenDir, 0774)
	}

	files, err := ioutil.ReadDir(workDir)
	if err != nil {
		return err
	}

	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") {
			r.JSONFiles = append(r.JSONFiles, f.Name())
		}
	}

	return nil
}

//CreateImages create images
func (r *Run) CreateImages() {
	for p := range r.PromExprs {
		if strings.Contains(p.Expr, "$") {
			continue
		}
		select {
		case <-r.Ctx.Done():
			return
		default:
			var v model.Value
			var err error
			for i := 0; i < 3; i++ {
				v, err = r.Client.Query(r.Ctx, p.Expr, r.From, r.To, time.Duration(p.Step)*time.Second)
				if err != nil && i == 2 {
					log.Errorf("expr %v can not get data from prometheus with error %v", p, err)
					continue
				}
				time.Sleep(1 * time.Second)
			}
			m, ok := v.(model.Matrix)
			if !ok {
				continue
			}
			m = r.FixErrorValue(m)
			w, err := plot.Plot(m, p.Name, "png")
			if err != nil {
				log.Errorf("can not write it with error %v", err)
				continue
			}
			if err := r.SaveImage(p.Name, w); err != nil {
				log.Errorf("can not write file %s with error %v", p.Name, err)
			}

		}
	}
}

//SaveImage save image file
func (r *Run) SaveImage(filename string, w io.WriterTo) error {
	fName := filepath.Join(r.ScreenDir, fmt.Sprintf("%s.png", filename))
	f, err := os.OpenFile(fName, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = w.WriteTo(f); err != nil {
		return err
	}
	return nil

}

//GetJSON http get
func GetJSON(dataFrom string) ([]byte, error) {
	if !strings.HasPrefix(dataFrom, "https://") {
		return ioutil.ReadFile(dataFrom)
	}

	for i := 0; i < 3; i++ {
		rsp, err := http.Get(dataFrom)
		if err != nil && i == 2 {
			return nil, err
		}
		if rsp.StatusCode == http.StatusOK {
			return ioutil.ReadAll(rsp.Body)
		}
	}

	return nil, errors.Errorf("can not get %s", dataFrom)
}

//getCPUNum get cpu number
func getCPUNum() int {
	return runtime.NumCPU()
}
