package gossip

import "testing"

func TestDataColumnSidecarTopicRoundTrip(t *testing.T) {
	for _, subnet := range []uint64{0, 1, 17, 63} {
		topic := DataColumnSidecarTopicString("0xdeadbeef", subnet)
		want := "/eth2/0xdeadbeef/data_column_sidecar_" + itoa(subnet) + "/ssz_snappy"
		if topic != want {
			t.Errorf("topic = %q, want %q", topic, want)
		}
		got, err := ParseDataColumnSidecarTopic(topic)
		if err != nil {
			t.Fatalf("parse %q: %v", topic, err)
		}
		if got != subnet {
			t.Errorf("parsed subnet = %d, want %d", got, subnet)
		}
	}
}

func TestParseDataColumnSidecarTopicRejects(t *testing.T) {
	for _, topic := range []string{
		"/eth2/0xdeadbeef/beacon_block/ssz_snappy",
		"/eth2/0xdeadbeef/data_column_sidecar_/ssz_snappy",
		"/eth2/0xdeadbeef/data_column_sidecar_9x/ssz_snappy",
		"not-a-topic",
	} {
		if _, err := ParseDataColumnSidecarTopic(topic); err == nil {
			t.Errorf("topic %q must not parse", topic)
		}
	}
}

func TestParseGossipTopicDataColumnSidecar(t *testing.T) {
	topic, err := ParseGossipTopic("data_column_sidecar_5")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if topic != DataColumnSidecar {
		t.Fatalf("topic = %v, want DataColumnSidecar", topic)
	}
	if _, err := ParseGossipTopic("data_column_sidecar_"); err == nil {
		t.Error("empty subnet id must not parse")
	}
}

func TestComputeSubnetForDataColumnSidecar(t *testing.T) {
	if got := ComputeSubnetForDataColumnSidecar(64); got != 0 {
		t.Errorf("column 64 subnet = %d, want 0", got)
	}
	if got := ComputeSubnetForDataColumnSidecar(65); got != 1 {
		t.Errorf("column 65 subnet = %d, want 1", got)
	}
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
