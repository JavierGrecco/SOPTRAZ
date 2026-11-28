package trustedtime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/tannerryan/roughtime/protocol"
)

type rtpRecorder struct {
	mu sync.Mutex
	order []string
	nonces map[string]bool
	counts map[string]int
}

type rtpFake struct {
	id string
	conn net.PacketConn
	cert *protocol.Certificate
	recorder *rtpRecorder
	radius time.Duration
	replay, corrupt bool
	mu sync.Mutex
	firstReply []byte
	done chan struct{}
}

func newRTPFake(t *testing.T, id string, root, online ed25519.PrivateKey, recorder *rtpRecorder, opts ...func(*rtpFake)) (*rtpFake, ed25519.PublicKey) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	base := time.Now()
	cert, err := protocol.NewCertificate(base.Add(-time.Hour), base.Add(time.Hour), online, root)
	if err != nil { _ = conn.Close(); t.Fatal(err) }
	server := &rtpFake{id:id, conn:conn, cert:cert, recorder:recorder, radius:100*time.Millisecond, done:make(chan struct{})}
	for _, opt := range opts { opt(server) }
	go server.serve(t, base)
	t.Cleanup(func(){ _ = conn.Close(); <-server.done; cert.Wipe() })
	return server, root.Public().(ed25519.PublicKey)
}

func (s *rtpFake) serve(t *testing.T, base time.Time) {
	defer close(s.done)
	buf := make([]byte, 64*1024)
	for {
		n, addr, err := s.conn.ReadFrom(buf)
		if err != nil { return }
		req, err := protocol.ParseRequest(buf[:n])
		if err != nil { t.Errorf("parse test request: %v", err); continue }
		if !reflect.DeepEqual(req.Versions, []protocol.Version{protocol.Version1}) {
			t.Errorf("requested versions = %v, want RFC 10049 v1", req.Versions)
			continue
		}
		s.recorder.mu.Lock()
		s.recorder.order = append(s.recorder.order, s.id)
		s.recorder.counts[s.id]++
		if s.recorder.nonces[string(req.Nonce)] {
			s.recorder.mu.Unlock()
			t.Errorf("request nonce reused")
			continue
		}
		s.recorder.nonces[string(req.Nonce)] = true
		s.recorder.mu.Unlock()

		s.mu.Lock()
		var reply []byte
		if s.replay && s.firstReply != nil {
			reply = append([]byte(nil), s.firstReply...)
		} else {
			midpoint := base.Add(time.Since(base))
			replies, e := protocol.CreateReplies(protocol.Version1, []protocol.Request{*req}, midpoint, s.radius, s.cert)
			if e != nil { s.mu.Unlock(); t.Errorf("create reply: %v", e); continue }
			reply = replies[0]
			if s.firstReply == nil { s.firstReply = append([]byte(nil), reply...) }
			if s.corrupt { reply = append([]byte(nil), reply...); reply[len(reply)-1] ^= 1 }
		}
		s.mu.Unlock()
		if _, err := s.conn.WriteTo(reply, addr); err != nil { return }
	}
}

func newRTPWitness(t *testing.T, id, operator string, rec *rtpRecorder, opts ...func(*rtpFake)) (RoughtimeWitness, *rtpFake) {
	t.Helper()
	_, root, err := ed25519.GenerateKey(rand.Reader)
	if err != nil { t.Fatal(err) }
	_, online, err := ed25519.GenerateKey(rand.Reader)
	if err != nil { t.Fatal(err) }
	server, pub := newRTPFake(t, id, root, online, rec, opts...)
	sum := sha256.Sum256(pub)
	return RoughtimeWitness{
		Source: Source{ID:id, OperatorID:operator, Kind:SourceRoughtime, Assurance:AssuranceAttested,
			TrustAnchorID:"lab-anchor-"+id, KeyID:hex.EncodeToString(sum[:])},
		PublicKey:append([]byte(nil), pub...),
		Endpoints:[]RoughtimeEndpoint{{Transport:"udp", Address:server.conn.LocalAddr().String()}},
	}, server
}

func newRTPRecorder() *rtpRecorder {
	return &rtpRecorder{nonces:map[string]bool{}, counts:map[string]int{}}
}

func TestRoughtimeProviderTwoPassesCausalChainAndEvidence(t *testing.T) {
	rec := newRTPRecorder()
	witnesses := make([]RoughtimeWitness, 0, 3)
	for i:=0; i<3; i++ {
		w, _ := newRTPWitness(t, fmt.Sprintf("w-%d",i), fmt.Sprintf("op-%d",i), rec)
		witnesses = append(witnesses,w)
	}
	p, err := NewRoughtimeProvider(RoughtimeProviderConfig{Witnesses:witnesses, ExchangeTimeout:time.Second, MeasurementTimeout:15*time.Second, MaxRadius:2*time.Second})
	if err != nil { t.Fatal(err) }
	got, err := p.ObserveBatch(context.Background())
	if err != nil { t.Fatal(err) }
	if len(got)!=3 { t.Fatalf("evidence count %d, want 3",len(got)) }
	for _, ev := range got {
		if err := ev.Validate(); err != nil { t.Errorf("invalid Evidence: %v",err) }
		if len(ev.EvidenceSHA256)!=64 || ev.Source.Assurance!=AssuranceAttested { t.Errorf("bad provenance: %#v",ev) }
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.order)!=6 || !reflect.DeepEqual(rec.order[:3],rec.order[3:]) { t.Fatalf("two passes differ: %v",rec.order) }
	if len(rec.nonces)!=6 { t.Fatalf("unique nonce count %d, want 6",len(rec.nonces)) }
	for _, w := range witnesses { if rec.counts[w.Source.ID]!=2 { t.Errorf("%s count %d",w.Source.ID,rec.counts[w.Source.ID]) } }
	policy := policyTestConfig(witnesses[0].Source,witnesses[1].Source,witnesses[2].Source)
	policy.MaxAggregateUncertainty=5*time.Second
	if _,err:=policy.Accept(got,nil);err!=nil { t.Fatalf("policy rejected provider evidence: %v",err) }
}

func TestRoughtimeProviderRejectsReplayAndBadSignature(t *testing.T) {
	for _, mode := range []string{"replay","signature"} {
		t.Run(mode,func(t *testing.T){
			rec:=newRTPRecorder()
			ws:=make([]RoughtimeWitness,0,3)
			for i:=0;i<3;i++ {
				opts:=[]func(*rtpFake){}
				if i==1 && mode=="replay" { opts=append(opts,func(s *rtpFake){s.replay=true}) }
				if i==0 && mode=="signature" { opts=append(opts,func(s *rtpFake){s.corrupt=true}) }
				w,_:=newRTPWitness(t,fmt.Sprintf("w-%d",i),fmt.Sprintf("op-%d",i),rec,opts...)
				ws=append(ws,w)
			}
			p,err:=NewRoughtimeProvider(RoughtimeProviderConfig{Witnesses:ws,ExchangeTimeout:time.Second,MeasurementTimeout:15*time.Second,MaxRadius:2*time.Second})
			if err!=nil {t.Fatal(err)}
			got,err:=p.ObserveBatch(context.Background())
			if !errors.Is(err,ErrRoughtimeVerification)||got!=nil {t.Fatalf("got (%v,%v), want nil evidence and verification error",got,err)}
		})
	}
}

func TestRoughtimeProviderRejectsRadiusAbovePolicy(t *testing.T) {
	rec:=newRTPRecorder()
	ws:=make([]RoughtimeWitness,0,3)
	for i:=0;i<3;i++ {
		opts:=[]func(*rtpFake){}
		if i==0 {opts=append(opts,func(s *rtpFake){s.radius=5*time.Second})}
		w,_:=newRTPWitness(t,fmt.Sprintf("w-%d",i),fmt.Sprintf("op-%d",i),rec,opts...)
		ws=append(ws,w)
	}
	p,err:=NewRoughtimeProvider(RoughtimeProviderConfig{Witnesses:ws,ExchangeTimeout:time.Second,MeasurementTimeout:15*time.Second,MaxRadius:2*time.Second})
	if err!=nil {t.Fatal(err)}
	got,err:=p.ObserveBatch(context.Background())
	if !errors.Is(err,ErrRoughtimeRadius)||got!=nil {t.Fatalf("got (%v,%v), want nil evidence and radius error",got,err)}
}

func TestNewRoughtimeProviderRejectsInvalidTrustConfiguration(t *testing.T) {
	rec:=newRTPRecorder()
	ws:=make([]RoughtimeWitness,0,3)
	for i:=0;i<3;i++ {w,_:=newRTPWitness(t,fmt.Sprintf("w-%d",i),fmt.Sprintf("op-%d",i),rec);ws=append(ws,w)}
	valid:=func()RoughtimeProviderConfig{return RoughtimeProviderConfig{Witnesses:append([]RoughtimeWitness(nil),ws...),ExchangeTimeout:time.Second,MeasurementTimeout:15*time.Second,MaxRadius:2*time.Second}}
	tests:=[]struct{name string;edit func(*RoughtimeProviderConfig)}{
		{"fewer than three",func(c *RoughtimeProviderConfig){c.Witnesses=c.Witnesses[:2]}},
		{"same operator",func(c *RoughtimeProviderConfig){c.Witnesses[1].Source.OperatorID=c.Witnesses[0].Source.OperatorID}},
		{"wrong key fingerprint",func(c *RoughtimeProviderConfig){c.Witnesses[0].Source.KeyID="wrong"}},
		{"duplicate root",func(c *RoughtimeProviderConfig){c.Witnesses[1].PublicKey=append([]byte(nil),c.Witnesses[0].PublicKey...);h:=sha256.Sum256(c.Witnesses[1].PublicKey);c.Witnesses[1].Source.KeyID=hex.EncodeToString(h[:])}},
		{"unsupported transport",func(c *RoughtimeProviderConfig){c.Witnesses[0].Endpoints[0].Transport="https"}},
		{"invalid port",func(c *RoughtimeProviderConfig){c.Witnesses[0].Endpoints[0].Address="localhost:70000"}},
	}
	for _,tt:=range tests {t.Run(tt.name,func(t *testing.T){c:=valid();tt.edit(&c);if _,err:=NewRoughtimeProvider(c);!errors.Is(err,ErrInvalidRoughtimeConfig){t.Fatalf("error=%v",err)}})}
}

func TestRoughtimeProviderHonorsContextDeadline(t *testing.T) {
	ws:=make([]RoughtimeWitness,0,3)
	listeners:=make([]net.PacketConn,0,3)
	for i:=0;i<3;i++ {
		conn,err:=net.ListenPacket("udp","127.0.0.1:0")
		if err!=nil {t.Fatal(err)}
		listeners=append(listeners,conn)
		_,root,err:=ed25519.GenerateKey(rand.Reader)
		if err!=nil {t.Fatal(err)}
		pub:=root.Public().(ed25519.PublicKey)
		h:=sha256.Sum256(pub)
		id:=fmt.Sprintf("silent-%d",i)
		ws=append(ws,RoughtimeWitness{
			Source:Source{ID:id,OperatorID:"operator-"+id,Kind:SourceRoughtime,Assurance:AssuranceAttested,TrustAnchorID:"anchor-"+id,KeyID:hex.EncodeToString(h[:])},
			PublicKey:append([]byte(nil),pub...),Endpoints:[]RoughtimeEndpoint{{Transport:"udp",Address:conn.LocalAddr().String()}},
		})
	}
	defer func(){for _,l:=range listeners{_ = l.Close()}}()
	p,err:=NewRoughtimeProvider(RoughtimeProviderConfig{Witnesses:ws,ExchangeTimeout:5*time.Second,MeasurementTimeout:5*time.Second,MaxRadius:2*time.Second})
	if err!=nil {t.Fatal(err)}
	ctx,cancel:=context.WithTimeout(context.Background(),100*time.Millisecond)
	defer cancel()
	got,err:=p.ObserveBatch(ctx)
	if !errors.Is(err,context.DeadlineExceeded)||got!=nil {t.Fatalf("got (%v,%v), want nil evidence and deadline",got,err)}
}
