package main

import "testing"

func TestParseUsbTetherStatus(t *testing.T) {
	st, ok := parseUsbTetherStatus("RESULT=OK\nACTIVE=true\nIFACES=rndis0\n")
	if !ok {
		t.Fatal("expected RESULT=OK to be trusted")
	}
	if !st.Active || len(st.Ifaces) != 1 || st.Ifaces[0] != "rndis0" {
		t.Errorf("status = %+v", st)
	}
}

func TestParseUsbTetherStatusInactive(t *testing.T) {
	st, ok := parseUsbTetherStatus("RESULT=OK\nACTIVE=false\nIFACES=\n")
	if !ok {
		t.Fatal("expected RESULT=OK to be trusted")
	}
	if st.Active || len(st.Ifaces) != 0 {
		t.Errorf("status = %+v", st)
	}
}

func TestParseUsbTetherStatusFailure(t *testing.T) {
	if _, ok := parseUsbTetherStatus("RESULT=FAILED code=no_connectivity_manager\n"); ok {
		t.Fatal("must not trust a failed status query")
	}
	if _, ok := parseUsbTetherStatus(""); ok {
		t.Fatal("must not trust empty output")
	}
}
