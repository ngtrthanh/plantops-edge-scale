package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/ingress"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/engine"
)

type fakeWorkflow struct{
	snap engine.Snapshot
	camera []domain.CameraEvidence
}
func (f *fakeWorkflow) Snapshot()engine.Snapshot{return f.snap}
func (f *fakeWorkflow) ObservePosition(context.Context,domain.PositionSnapshot)error{return nil}
func (f *fakeWorkflow) ObserveFault(context.Context,domain.Fault)error{return nil}
func (f *fakeWorkflow) AuthorizeOverride(context.Context,domain.Override)error{return nil}
func (f *fakeWorkflow) ResetCompleted()error{return nil}
func (f *fakeWorkflow) ObserveCamera(_ context.Context,e domain.CameraEvidence)error{f.camera=append(f.camera,e);return nil}

func TestLPRImageAutoLinksDirectionalC1AndC3Evidence(t *testing.T){
	wf:=&fakeWorkflow{snap:engine.Snapshot{Transaction:&domain.Transaction{ID:"TX1",Direction:domain.DirectionAToB}}}
	s:=&Server{RFID:&ingress.RFID{},LPR:&ingress.LPR{},Workflow:wf,CameraIDs:map[string]bool{"C1A":true,"C1B":true,"C3":true}}
	h:=s.Handler()
	req:=httptest.NewRequest(http.MethodPost,"/io/lpr",bytes.NewBufferString(`{"plate":"15C-123.45","confidence":99,"image_ref":"c1a.jpg"}`));req.Header.Set("Content-Type","application/json")
	rr:=httptest.NewRecorder();h.ServeHTTP(rr,req);if rr.Code!=http.StatusAccepted{t.Fatalf("lpr status=%d body=%s",rr.Code,rr.Body.String())}
	if len(wf.camera)!=1||wf.camera[0].CameraID!="C1A"||wf.camera[0].ImageRef!="c1a.jpg"{t.Fatalf("evidence=%+v",wf.camera)}

	req=httptest.NewRequest(http.MethodPost,"/io/camera/C3",bytes.NewBufferString(`{"role":"OVERVIEW","image_ref":"overview.jpg"}`));rr=httptest.NewRecorder();h.ServeHTTP(rr,req);if rr.Code!=http.StatusAccepted{t.Fatalf("C3 status=%d body=%s",rr.Code,rr.Body.String())}
	if len(wf.camera)!=2||wf.camera[1].CameraID!="C3"{t.Fatalf("evidence=%+v",wf.camera)}

	wf.snap.Transaction.Direction=domain.DirectionBToA
	req=httptest.NewRequest(http.MethodPost,"/io/lpr",bytes.NewBufferString(`{"plate":"15C-123.45","image_ref":"c1b.jpg"}`));rr=httptest.NewRecorder();h.ServeHTTP(rr,req)
	if len(wf.camera)!=3||wf.camera[2].CameraID!="C1B"{t.Fatalf("B->A evidence=%+v",wf.camera)}

	req=httptest.NewRequest(http.MethodPost,"/io/camera/C4",bytes.NewBufferString(`{"image_ref":"not-installed.jpg"}`));rr=httptest.NewRecorder();h.ServeHTTP(rr,req);if rr.Code!=http.StatusBadRequest{t.Fatalf("unconfigured camera status=%d",rr.Code)}
}

func TestLatestTicketAPI(t *testing.T){
	now:=time.Now().UTC();expected:=domain.Ticket{ID:"T1",CycleID:"C1",NetKG:16640,CommittedAt:now}
	s:=&Server{RFID:&ingress.RFID{},LPR:&ingress.LPR{},LatestTicket:func(context.Context)(domain.Ticket,bool,error){return expected,true,nil}}
	rr:=httptest.NewRecorder();s.Handler().ServeHTTP(rr,httptest.NewRequest(http.MethodGet,"/api/tickets/latest",nil));if rr.Code!=http.StatusOK||!bytes.Contains(rr.Body.Bytes(),[]byte(`"net_kg":16640`)){t.Fatalf("status=%d body=%s",rr.Code,rr.Body.String())}
}
