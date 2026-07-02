package main

import "testing"

// A trimmed but representative `dumpsys telephony.registry` LTE snapshot, using
// the exact field formats observed on the SM-F731B (VN Vinaphone, band 3).
const lteDump = `mTelephonyDisplayInfo=TelephonyDisplayInfo{network=LTE, overrideNetwork=LTE_CA}
mServiceState={mOperatorAlphaLong=VN VINAPHONE, getRilDataRadioTechnology=14(LTE), isUsingCarrierAggregation=true,
 nrState=NONE, cellIdentity=CellIdentityLte:{ mCi=12345 mPci=481 mTac=6408 mEarfcn=1300 mBands=[3] mBandwidth=2147483647 mMcc=452 mMnc=02 mAdditionalPlmns={} mCsgInfo=null}}
mSignalStrength=SignalStrength:{ CellSignalStrengthLte: rssi=-51 rsrp=-84 rsrq=-9 rssnr=24 cqiTableIndex=2147483647 cqi=2147483647 ta=2147483647 level=4 parametersUseForLevel=0 }`

func TestParseSignalLTE(t *testing.T) {
	s := parseSignal(lteDump)
	if !s.Available {
		t.Fatal("expected available")
	}
	if s.Tech != "LTE" { // display network= wins
		t.Errorf("tech=%q want LTE", s.Tech)
	}
	if s.Rsrp != -84 || s.Rsrq != -9 || s.Sinr != 24 || s.Rssi != -51 || s.Level != 4 {
		t.Errorf("lte sig wrong: %+v", s)
	}
	if s.Band != 3 || s.Earfcn != 1300 || s.PCI != 481 || s.TAC != 6408 {
		t.Errorf("lte cell wrong: band=%d earfcn=%d pci=%d tac=%d", s.Band, s.Earfcn, s.PCI, s.TAC)
	}
	if s.Mcc != "452" || s.Mnc != "02" {
		t.Errorf("plmn wrong: mcc=%q mnc=%q", s.Mcc, s.Mnc)
	}
	if !s.CA {
		t.Error("expected carrier_aggregation true")
	}
	if s.Operator != "VN VINAPHONE" {
		t.Errorf("operator=%q", s.Operator)
	}
}

func TestParseSignalUnavailableSentinel(t *testing.T) {
	// cqi/ta are Integer.MAX_VALUE; they must not leak into any parsed field.
	s := parseSignal(lteDump)
	if s.Band == intUnavail {
		t.Fatal("sentinel leaked")
	}
}

const nrDump = `mServiceState={getRilDataRadioTechnology=20(NR), isUsingCarrierAggregation=false, nrState=CONNECTED,
 cellIdentity=CellIdentityNr:{ mPci=234 mTac=5 mNrArfcn=632448 mBands=[78] mMcc=452 mMnc=02 mAdditionalPlmns={}}}
mSignalStrength=SignalStrength:{ CellSignalStrengthLte: rssi=-70 rsrp=-95 rsrq=-11 rssnr=10 cqiTableIndex=2147483647 cqi=2147483647 ta=1 level=3
 CellSignalStrengthNr:{ csiRsrp = 2147483647 csiRsrq = 2147483647 csiSinr = 2147483647 ssRsrp = -88 ssRsrq = -12 ssSinr = 15 level = 4 } }`

func TestParseSignalNR(t *testing.T) {
	s := parseSignal(nrDump)
	if s.NrState != "CONNECTED" {
		t.Errorf("nr_state=%q", s.NrState)
	}
	if s.NrRsrp != -88 || s.NrRsrq != -12 || s.NrSinr != 15 {
		t.Errorf("nr sig wrong: rsrp=%d rsrq=%d sinr=%d", s.NrRsrp, s.NrRsrq, s.NrSinr)
	}
	if s.NrBand != 78 || s.NrArfcn != 632448 {
		t.Errorf("nr cell wrong: band=%d arfcn=%d", s.NrBand, s.NrArfcn)
	}
}

func TestParseSignalEmpty(t *testing.T) {
	if s := parseSignal(""); s.Available {
		t.Error("empty dump must be unavailable")
	}
}
