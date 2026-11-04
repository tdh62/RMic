package virtual

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrEndpointMissing = errors.New("双向音频设备已失效或无法识别")

type pulseEndpoint struct {
	Index         uint32 `json:"index"`
	Name          string `json:"name"`
	MonitorSource any    `json:"monitor_source"`
}
type pulseModule struct {
	Name     string `json:"name"`
	Argument string `json:"argument"`
}
type graph map[string][]string

func (g graph) add(from, to string) {
	if from != "" && to != "" {
		g[from] = append(g[from], to)
	}
}
func (g graph) reaches(from, to string) bool {
	seen := map[string]bool{}
	var visit func(string) bool
	visit = func(node string) bool {
		if node == to {
			return true
		}
		if seen[node] {
			return false
		}
		seen[node] = true
		for _, next := range g[node] {
			if visit(next) {
				return true
			}
		}
		return false
	}
	return visit(from)
}
func arguments(value string) map[string]string {
	result := map[string]string{}
	for _, field := range strings.Fields(value) {
		if key, val, ok := strings.Cut(field, "="); ok {
			result[key] = strings.Trim(val, "\"'")
		}
	}
	return result
}

// CheckRoutes follows PulseAudio monitor/remap/loopback edges instead of
// comparing display names: the two ends of a cable have different names.
func CheckRoutes(outputSink, captureSource string) error {
	var sinks, sources []pulseEndpoint
	var modules []pulseModule
	for _, entry := range []struct {
		kind   string
		target any
	}{{"sinks", &sinks}, {"sources", &sources}, {"modules", &modules}} {
		data, err := run("-f", "json", "list", entry.kind)
		if err != nil {
			return err
		}
		if err = json.Unmarshal([]byte(data), entry.target); err != nil {
			return fmt.Errorf("read PulseAudio %s: %w", entry.kind, err)
		}
	}
	return checkGraph(sinks, sources, modules, outputSink, captureSource)
}
func checkGraph(sinks, sources []pulseEndpoint, modules []pulseModule, output, capture string) error {
	sourceByIndex := map[uint32]string{}
	sourceExists, sinkExists := false, false
	for _, source := range sources {
		sourceByIndex[source.Index] = source.Name
		if source.Name == capture {
			sourceExists = true
		}
	}
	g := graph{}
	for _, sink := range sinks {
		if sink.Name == output {
			sinkExists = true
		}
		var monitor string
		switch value := sink.MonitorSource.(type) {
		case string:
			monitor = value
		case float64:
			monitor = sourceByIndex[uint32(value)]
		}
		if monitor != "" {
			g.add("sink:"+sink.Name, "source:"+monitor)
		}
	}
	if !sourceExists || !sinkExists {
		return fmt.Errorf("%w；不会改用默认设备", ErrEndpointMissing)
	}
	for _, module := range modules {
		a := arguments(module.Argument)
		switch module.Name {
		case "module-remap-source":
			if a["master"] == "" || a["source_name"] == "" {
				return fmt.Errorf("存在无法识别 master/source_name 的 remap-source，无法验证双向路由")
			}
			g.add("source:"+a["master"], "source:"+a["source_name"])
		case "module-remap-sink":
			if a["master"] == "" || a["sink_name"] == "" {
				return fmt.Errorf("存在无法识别 master/sink_name 的 remap-sink，无法验证双向路由")
			}
			g.add("sink:"+a["sink_name"], "sink:"+a["master"])
		case "module-combine-sink":
			for _, sink := range strings.Split(a["slaves"], ",") {
				if sink != "" {
					g.add("sink:"+a["sink_name"], "sink:"+sink)
				}
			}
		case "module-loopback":
			if a["source"] == "" || a["sink"] == "" {
				return fmt.Errorf("存在使用默认设备的 loopback，无法验证双向路由；请显式配置其 source 和 sink")
			}
			source := a["source"]
			if index, err := strconv.ParseUint(source, 10, 32); err == nil {
				source = sourceByIndex[uint32(index)]
			}
			sink := a["sink"]
			if index, err := strconv.ParseUint(sink, 10, 32); err == nil {
				for _, entry := range sinks {
					if entry.Index == uint32(index) {
						sink = entry.Name
					}
				}
			}
			g.add("source:"+source, "sink:"+sink)
		}
	}
	if g.reaches("sink:"+output, "source:"+capture) {
		return fmt.Errorf("检测到音频反馈路径：手机麦克风输出可回到电脑采集源；请使用两条独立虚拟通道")
	}
	return nil
}
