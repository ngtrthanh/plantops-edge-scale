package centralsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/sqlitestore"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

func TestWorkerAcksDurableTicketAfterHTTP2xx(t *testing.T){
	ctx:=context.Background();s,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"edge.db"));if err!=nil{t.Fatal(err)};defer s.Close()
	now:=time.Now().UTC();ticket:=domain.Ticket{ID:"T1",StationID:"S1",TransactionID:"TX1",Plate:"15C-1",RFID:"R1",WeightKG:1000,WeightObservedAt:now,WeightRawRef:domain.RawWeightRef{Seq:1,Hash:"h"},Mode:domain.ModeNormal,CommittedAt:now}
	if err:=s.Commit(ctx,ticket);err!=nil{t.Fatal(err)}
	var got atomic.Int32
	srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){if r.Header.Get("Authorization")!="Bearer secret"{t.Errorf("auth=%q",r.Header.Get("Authorization"))};got.Add(1);w.WriteHeader(http.StatusAccepted)}));defer srv.Close()
	w:=&Worker{Store:s,Config:Config{TicketURL:srv.URL,BearerToken:"secret",StationID:"S1"}}
	if err:=w.syncOnce(ctx);err!=nil{t.Fatal(err)}
	q,err:=s.PendingSync(ctx,10);if err!=nil{t.Fatal(err)};if len(q)!=0{t.Fatalf("pending=%d",len(q))};if got.Load()!=1{t.Fatalf("requests=%d",got.Load())}
}

func TestWorkerKeepsPendingOnFailure(t *testing.T){
	ctx:=context.Background();s,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"edge.db"));if err!=nil{t.Fatal(err)};defer s.Close()
	now:=time.Now().UTC();ticket:=domain.Ticket{ID:"T2",StationID:"S1",TransactionID:"TX2",Plate:"15C-2",RFID:"R2",WeightKG:1000,WeightObservedAt:now,WeightRawRef:domain.RawWeightRef{Seq:2,Hash:"h2"},Mode:domain.ModeNormal,CommittedAt:now}
	if err:=s.Commit(ctx,ticket);err!=nil{t.Fatal(err)}
	srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){http.Error(w,"offline",http.StatusServiceUnavailable)}));defer srv.Close()
	w:=&Worker{Store:s,Config:Config{TicketURL:srv.URL,StationID:"S1"}}
	if err:=w.syncOnce(ctx);err!=nil{t.Fatal(err)}
	q,err:=s.PendingSync(ctx,10);if err!=nil{t.Fatal(err)};if len(q)!=1||q[0].AttemptCount!=1{t.Fatalf("queue=%+v",q)}
}
