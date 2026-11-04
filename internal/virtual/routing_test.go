package virtual

import "testing"

func TestRoutingRejectsDirectAndIndirectFeedback(t *testing.T) {
	sinks := []pulseEndpoint{{Index: 1, Name: "mic", MonitorSource: "mic.monitor"}, {Index: 2, Name: "computer", MonitorSource: "computer.monitor"}}
	sources := []pulseEndpoint{{Index: 10, Name: "mic.monitor"}, {Index: 11, Name: "mic_input"}, {Index: 20, Name: "computer.monitor"}, {Index: 21, Name: "computer_capture"}}
	modules := []pulseModule{{Name: "module-remap-source", Argument: "master=mic.monitor source_name=mic_input"}, {Name: "module-remap-source", Argument: "master=computer.monitor source_name=computer_capture"}}
	if err := checkGraph(sinks, sources, modules, "mic", "computer_capture"); err != nil {
		t.Fatal(err)
	}
	if err := checkGraph(sinks, sources, modules, "mic", "mic_input"); err == nil {
		t.Fatal("different names for the same cable bypassed routing protection")
	}
	withLoopback := append(append([]pulseModule(nil), modules...), pulseModule{Name: "module-loopback", Argument: "source=mic_input sink=computer"})
	if err := checkGraph(sinks, sources, withLoopback, "mic", "computer_capture"); err == nil {
		t.Fatal("indirect loopback was accepted")
	}
	withDefault := append(append([]pulseModule(nil), modules...), pulseModule{Name: "module-loopback", Argument: "sink=computer"})
	if err := checkGraph(sinks, sources, withDefault, "mic", "computer_capture"); err == nil {
		t.Fatal("ambiguous default loopback was accepted")
	}
	if err := checkGraph(sinks, sources, modules, "mic", "missing"); err == nil {
		t.Fatal("missing capture device was accepted")
	}
}

func TestRoutingHandlesNumericMonitorAndCycles(t *testing.T) {
	sinks := []pulseEndpoint{{Name: "mic", MonitorSource: float64(10)}, {Name: "other", MonitorSource: "other.monitor"}}
	sources := []pulseEndpoint{{Index: 10, Name: "mic.monitor"}, {Index: 11, Name: "alias"}, {Index: 12, Name: "other.monitor"}}
	modules := []pulseModule{{Name: "module-remap-source", Argument: "master=mic.monitor source_name=alias"}, {Name: "module-loopback", Argument: "source=alias sink=mic"}}
	if err := checkGraph(sinks, sources, modules, "mic", "alias"); err == nil {
		t.Fatal("numeric monitor reference was missed")
	}
	if err := checkGraph(sinks, sources, modules, "mic", "other.monitor"); err != nil {
		t.Fatalf("unrelated route rejected: %v", err)
	}
}
