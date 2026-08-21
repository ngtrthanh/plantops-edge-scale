package centralsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/sqlitestore"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/ports"
)

type Store interface {
	PendingSync(context.Context, int) ([]sqlitestore.SyncItem, error)
	MarkSyncAttempt(context.Context, int64, string, time.Time) error
	AckSync(context.Context, int64) error
}

type Config struct {
	TicketURL     string
	HeartbeatURL  string
	BearerToken   string
	StationID     string
	Version       string
	GitSHA        string
	Interval      time.Duration
	HeartbeatEvery time.Duration
	Batch         int
	Timeout       time.Duration
}

type Worker struct {
	Store Store
	Audit ports.AuditStore
	Client *http.Client
	Config Config
}

func (w *Worker) Run(ctx context.Context) error {
	if w.Store == nil { return errors.New("central sync store is nil") }
	if w.Config.TicketURL == "" && w.Config.HeartbeatURL == "" { return nil }
	interval:=w.Config.Interval;if interval<=0{interval=5*time.Second}
	heartbeatEvery:=w.Config.HeartbeatEvery;if heartbeatEvery<=0{heartbeatEvery=30*time.Second}
	tick:=time.NewTicker(interval);defer tick.Stop()
	hb:=time.NewTicker(heartbeatEvery);defer hb.Stop()
	_ = w.syncOnce(ctx)
	_ = w.heartbeat(ctx)
	for {
		select {
		case <-ctx.Done(): return nil
		case <-tick.C: _ = w.syncOnce(ctx)
		case <-hb.C: _ = w.heartbeat(ctx)
		}
	}
}

func (w *Worker) syncOnce(ctx context.Context) error {
	if w.Config.TicketURL==""{return nil}
	batch:=w.Config.Batch;if batch<=0||batch>100{batch=20}
	items,err:=w.Store.PendingSync(ctx,batch);if err!=nil{return err}
	for _,item:=range items{
		if item.Kind!="TICKET"{continue}
		_ = w.audit(ctx,domain.AuditCentralSyncAttempt,item.EntityID,"attempt",map[string]any{"queue_id":item.ID,"attempt":item.AttemptCount+1})
		status,sendErr:=w.post(ctx,w.Config.TicketURL,[]byte(item.PayloadJSON))
		if sendErr!=nil||status<200||status>=300{
			msg:="";if sendErr!=nil{msg=sendErr.Error()}else{msg=fmt.Sprintf("HTTP %d",status)}
			next:=time.Now().UTC().Add(backoff(item.AttemptCount+1))
			_ = w.Store.MarkSyncAttempt(ctx,item.ID,msg,next)
			continue
		}
		if err:=w.Store.AckSync(ctx,item.ID);err!=nil{return err}
		_ = w.audit(ctx,domain.AuditCentralSyncAck,item.EntityID,"acked",map[string]any{"queue_id":item.ID,"http_status":status})
	}
	return nil
}

func (w *Worker) heartbeat(ctx context.Context) error {
	if w.Config.HeartbeatURL==""{return nil}
	payload,_:=json.Marshal(map[string]any{"station_id":w.Config.StationID,"version":w.Config.Version,"git_sha":w.Config.GitSHA,"at_utc":time.Now().UTC()})
	status,err:=w.post(ctx,w.Config.HeartbeatURL,payload)
	if err==nil&&status>=200&&status<300{_ = w.audit(ctx,domain.AuditHeartbeat,"","sent",map[string]any{"http_status":status})}
	if err!=nil{return err};if status<200||status>=300{return fmt.Errorf("heartbeat HTTP %d",status)};return nil
}

func (w *Worker) post(ctx context.Context,url string,payload []byte)(int,error){
	client:=w.Client;if client==nil{timeout:=w.Config.Timeout;if timeout<=0{timeout=5*time.Second};client=&http.Client{Timeout:timeout}}
	req,err:=http.NewRequestWithContext(ctx,http.MethodPost,url,bytes.NewReader(payload));if err!=nil{return 0,err}
	req.Header.Set("Content-Type","application/json")
	if w.Config.BearerToken!=""{req.Header.Set("Authorization","Bearer "+w.Config.BearerToken)}
	resp,err:=client.Do(req);if err!=nil{return 0,err};defer resp.Body.Close();return resp.StatusCode,nil
}

func (w *Worker) audit(ctx context.Context,kind domain.AuditKind,txID,action string,data map[string]any)error{
	if w.Audit==nil{return nil}
	_,err:=w.Audit.Append(ctx,domain.AuditEvent{StationID:w.Config.StationID,TransactionID:txID,AtUTC:time.Now().UTC(),Kind:kind,Actor:"SYSTEM",Source:"CENTRAL_SYNC",Action:action,Data:data,RuntimeGitSHA:w.Config.GitSHA});return err
}

func backoff(attempt int)time.Duration{
	if attempt<1{attempt=1};d:=time.Duration(1<<min(attempt-1,6))*5*time.Second;if d>5*time.Minute{return 5*time.Minute};return d
}
func min(a,b int)int{if a<b{return a};return b}
