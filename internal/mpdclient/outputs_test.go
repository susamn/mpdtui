package mpdclient

import (
	"testing"

	"github.com/fhs/gompd/v2/mpd"
)

func TestParseOutputsTypesFields(t *testing.T) {
	rows := []mpd.Attrs{
		{"outputid": "0", "outputname": "My ALSA", "outputenabled": "1", "plugin": "alsa"},
		{"outputid": "1", "outputname": "cast", "outputenabled": "0", "plugin": "httpd", "attribute": "port=8000"},
	}

	outs := parseOutputs(rows)
	if len(outs) != 2 {
		t.Fatalf("got %d outputs, want 2", len(outs))
	}

	if outs[0].ID != 0 || outs[0].Name != "My ALSA" || !outs[0].Enabled || outs[0].Plugin != "alsa" {
		t.Errorf("outs[0] = %+v, want the alsa output enabled", outs[0])
	}
	if outs[1].ID != 1 || outs[1].Enabled || outs[1].Plugin != "httpd" {
		t.Errorf("outs[1] = %+v, want id 1 httpd disabled", outs[1])
	}
	if outs[1].Attrs["port"] != "8000" {
		t.Errorf("outs[1].Attrs = %v, want port=8000", outs[1].Attrs)
	}
}

func TestParseOutputsSkipsRowsWithoutID(t *testing.T) {
	outs := parseOutputs([]mpd.Attrs{{"outputname": "orphan"}, {"outputid": "notanumber"}})
	if len(outs) != 0 {
		t.Errorf("got %d outputs, want 0 (rows without a numeric outputid are skipped)", len(outs))
	}
}

func TestParseOutputsEnabledOnlyForLiteralOne(t *testing.T) {
	outs := parseOutputs([]mpd.Attrs{
		{"outputid": "0", "outputenabled": ""},
		{"outputid": "1", "outputenabled": "0"},
		{"outputid": "2", "outputenabled": "1"},
	})
	if outs[0].Enabled || outs[1].Enabled || !outs[2].Enabled {
		t.Errorf("enabled flags = %v/%v/%v, want false/false/true", outs[0].Enabled, outs[1].Enabled, outs[2].Enabled)
	}
}
