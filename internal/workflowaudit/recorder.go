package workflowaudit

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/engine"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/ports"
)

type Workflow interface {
	Snapshot() engine.Snapshot
	ActiveTransactionID() string
	ObservePosition(context.Context, domain.PositionSnapshot) error
	ObserveRFID(context.Context, domain.RFIDObservation) error
	ObserveLPR(context.Context, domain.LPRObservation) error
	ObserveCamera(context.Context, domain.CameraEvidence) error
	ObserveScale(context.Context, domain.AuditedScaleReading) error
	ObserveFault(context.Context, domain.Fault) error
	ClearFault(context.Context, domain.DeviceID) error
	AuthorizeOverride(context.Context, domain.Override) error
	ResetCompleted() error
}

type Recorder struct {
	Engine Workflow
	Audit ports.AuditStore
	StationID string
	RuntimeGitSHA string
	mu sync.Mutex
}

func (r *Recorder) Snapshot() engine.Snapshot{return r.Engine.Snapshot()}
func (r *Recorder) ActiveTransactionID()string{return r.Engine.ActiveTransactionID()}
func (r *Recorder) ObservePosition(ctx context.Context,p domain.PositionSnapshot)error{return r.record(ctx,func()error{return r.Engine.ObservePosition(ctx,p)})}
func (r *Recorder) ObserveRFID(ctx context.Context,o domain.RFIDObservation)error{return r.record(ctx,func()error{return r.Engine.ObserveRFID(ctx,o)})}
func (r *Recorder) ObserveLPR(ctx context.Context,o domain.LPRObservation)error{return r.record(ctx,func()error{return r.Engine.ObserveLPR(ctx,o)})}
func (r *Recorder) ObserveCamera(ctx context.Context,o domain.CameraEvidence)error{return r.record(ctx,func()error{return r.Engine.ObserveCamera(ctx,o)})}
func (r *Recorder) ObserveScale(ctx context.Context,a domain.AuditedScaleReading)error{return r.record(ctx,func()error{return r.Engine.ObserveScale(ctx,a)})}
func (r *Recorder) ObserveFault(ctx context.Context,f domain.Fault)error{return r.record(ctx,func()error{return r.Engine.ObserveFault(ctx,f)})}
func (r *Recorder) ClearFault(ctx context.Context,d domain.DeviceID)error{return r.record(ctx,func()error{return r.Engine.ClearFault(ctx,d)})}
func (r *Recorder) AuthorizeOverride(ctx context.Context,o domain.Override)error{return r.record(ctx,func()error{return r.Engine.AuthorizeOverride(ctx,o)})}
func (r *Recorder) ResetCompleted()error{return r.record(context.Background(),r.Engine.ResetCompleted)}

func (r *Recorder) record(ctx context.Context,action func()error)error{
	if r.Engine==nil{return fmt.Errorf("workflow audit recorder engine is nil")};if r.Audit==nil{return fmt.Errorf("workflow audit recorder store is nil")}
	r.mu.Lock();defer r.mu.Unlock();before:=r.Engine.Snapshot();actionErr:=action();after:=r.Engine.Snapshot()
	for _,event:=range diff(before,after){if err:=r.append(ctx,event);err!=nil{_ = r.Engine.ObserveFault(context.Background(),domain.Fault{Device:domain.DeviceAuditStore,Health:domain.HealthFault,Reason:"operational audit append failed: "+err.Error(),Overridable:false,Critical:true});if actionErr!=nil{return fmt.Errorf("%v; operational audit: %w",actionErr,err)};return fmt.Errorf("operational audit: %w",err)}}
	return actionErr
}
func (r *Recorder) append(ctx context.Context,event domain.AuditEvent)error{if event.StationID==""{event.StationID=r.StationID};if event.AtUTC.IsZero(){event.AtUTC=time.Now().UTC()};if event.Actor==""{event.Actor="SYSTEM"};if event.RuntimeGitSHA==""{event.RuntimeGitSHA=r.RuntimeGitSHA};_,err:=r.Audit.Append(ctx,event);return err}

func diff(prev,cur engine.Snapshot)[]domain.AuditEvent{
	out:=make([]domain.AuditEvent,0,16);pt,ct:=prev.Transaction,cur.Transaction
	if pt!=nil&&ct==nil{return append(out,domain.AuditEvent{TransactionID:pt.ID,Kind:domain.AuditTransactionReset,Source:"WORKFLOW",Action:"physical pass reset to IDLE",OldState:pt.State,NewState:domain.StateIdle,Data:map[string]any{"direction":pt.Direction,"cycle_id":pt.CycleID,"cycle_status":pt.CycleStatus,"business_complete":pt.BusinessComplete}})}
	if pt==nil&&ct!=nil{out=append(out,domain.AuditEvent{TransactionID:ct.ID,Kind:domain.AuditTransactionStarted,Source:"WORKFLOW",Action:"physical pass started",NewState:ct.State,Data:map[string]any{"direction":ct.Direction,"pass_number":ct.PassNumber}});pt=&domain.Transaction{ID:ct.ID}}
	if ct==nil{return out};txID:=ct.ID;if pt.ID!=txID{return out}
	if pt.State!=ct.State&&pt.State!=""{kind:=domain.AuditStateTransition;if ct.State==domain.StateComplete{kind=domain.AuditTransactionDone};out=append(out,domain.AuditEvent{TransactionID:txID,Kind:kind,Source:"WORKFLOW",Action:"state transition",OldState:pt.State,NewState:ct.State,Reason:ct.LastBlockReason,Data:map[string]any{"direction":ct.Direction,"pass_number":ct.PassNumber,"cycle_id":ct.CycleID,"cycle_status":ct.CycleStatus,"business_complete":ct.BusinessComplete}})}
	if pt.Direction!=ct.Direction||pt.PassNumber!=ct.PassNumber||pt.CycleID!=ct.CycleID||pt.CycleStatus!=ct.CycleStatus||pt.BusinessComplete!=ct.BusinessComplete{out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditStateTransition,Source:"CYCLE",Action:"business cycle binding changed",Reason:ct.LastBlockReason,Data:map[string]any{"direction":ct.Direction,"pass_number":ct.PassNumber,"cycle_id":ct.CycleID,"cycle_status":ct.CycleStatus,"business_complete":ct.BusinessComplete}})}
	if !reflect.DeepEqual(pt.RFID,ct.RFID)&&ct.RFID.Tag!=""{out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditRFIDObserved,Source:"RFID",Device:domain.DeviceRFID,Action:"observation",Data:map[string]any{"tag":ct.RFID.Tag,"quality":ct.RFID.Quality,"health":ct.RFID.Health,"observed_at":ct.RFID.Observed}})}
	if !reflect.DeepEqual(pt.LPR,ct.LPR)&&ct.LPR.Plate!=""{out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditLPRObserved,Source:"LPR",Device:domain.DeviceLPR,Action:"observation",Data:map[string]any{"plate":ct.LPR.Plate,"confidence":ct.LPR.Confidence,"image_ref":ct.LPR.ImageRef,"health":ct.LPR.Health,"observed_at":ct.LPR.Observed}})}
	if len(ct.CameraEvidence)>len(pt.CameraEvidence){for _,e:=range ct.CameraEvidence[len(pt.CameraEvidence):]{out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditCameraEvidence,Source:"CAMERA",Action:"evidence captured",Evidence:[]string{e.ImageRef},Data:map[string]any{"camera_id":e.CameraID,"role":e.Role,"captured_at":e.CapturedAt}})}}
	if pt.Identity!=ct.Identity||pt.IdentityReason!=ct.IdentityReason{out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditIdentityDecision,Source:"WORKFLOW",Action:string(ct.Identity),Reason:ct.IdentityReason,Data:map[string]any{"rfid":ct.RFID.Tag,"plate":ct.LPR.Plate,"direction":ct.Direction,"cycle_id":ct.CycleID}})}
	if pt.Position!=ct.Position||!reflect.DeepEqual(pt.PositionSnapshot,ct.PositionSnapshot){out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditPositionDecision,Source:"WORKFLOW",Action:string(ct.Position),Data:map[string]any{"position":ct.PositionSnapshot,"direction":ct.Direction}})}
	for _,f:=range addedFaults(pt.Faults,ct.Faults){out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditFaultSet,Source:"WORKFLOW",Device:f.Device,Action:"fault set",Reason:f.Reason,Data:map[string]any{"health":f.Health,"critical":f.Critical,"overridable":f.Overridable}})}
	for _,f:=range removedFaults(pt.Faults,ct.Faults){out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditFaultCleared,Source:"WORKFLOW",Device:f.Device,Action:"fault cleared",Reason:f.Reason})}
	if len(ct.Overrides)>len(pt.Overrides){for _,o:=range ct.Overrides[len(pt.Overrides):]{out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditOverrideAuthorized,Source:"WORKFLOW",Device:o.Device,Actor:o.AuthorizedBy,Action:"override authorized",Reason:o.Reason,Evidence:append([]string(nil),o.Evidence...),Data:map[string]any{"requested_by":o.RequestedBy,"authorized_as":o.AuthorizedAs,"authorized_at":o.AuthorizedAt}})}}
	if pt.AcceptedWeight==nil&&ct.AcceptedWeight!=nil{out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditStableAccepted,Source:"SCALE",Device:domain.DeviceScale,Action:"stable pass weight accepted",Data:map[string]any{"weight_kg":ct.AcceptedWeight.WeightKG,"observed_at":ct.AcceptedWeight.ObservedAt,"raw_seq":ct.AcceptedWeight.RawRef.Seq,"raw_hash":ct.AcceptedWeight.RawRef.Hash,"direction":ct.Direction,"pass_number":ct.PassNumber,"cycle_id":ct.CycleID}})}
	if pt.TicketID==""&&ct.TicketID!=""{out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditTicketCommitted,Source:"CYCLE",Action:"final paired business ticket committed",Data:map[string]any{"ticket_id":ct.TicketID,"cycle_id":ct.CycleID,"cycle_status":ct.CycleStatus,"business_complete":ct.BusinessComplete,"gross_kg":ct.GrossKG,"tare_kg":ct.TareKG,"net_kg":ct.NetKG,"committed_at":ct.LocalCommittedAt}})}
	if !reflect.DeepEqual(pt.Outputs,ct.Outputs){out=append(out,domain.AuditEvent{TransactionID:txID,Kind:domain.AuditDesiredOutputs,Source:"WORKFLOW",Action:"desired outputs changed",Data:map[string]any{"before":pt.Outputs,"after":ct.Outputs,"direction":ct.Direction}})}
	return out
}

func addedFaults(old,cur []domain.Fault)[]domain.Fault{return faultDiff(old,cur)}
func removedFaults(old,cur []domain.Fault)[]domain.Fault{return faultDiff(cur,old)}
func faultDiff(base,compare []domain.Fault)[]domain.Fault{seen:=map[string]bool{};for _,f:=range base{seen[faultKey(f)]=true};out:=[]domain.Fault{};for _,f:=range compare{if !seen[faultKey(f)]{out=append(out,f)}};sort.Slice(out,func(i,j int)bool{return string(out[i].Device)<string(out[j].Device)});return out}
func faultKey(f domain.Fault)string{return strings.Join([]string{string(f.Device),string(f.Health),f.Reason,fmt.Sprint(f.Critical),fmt.Sprint(f.Overridable)},"|")}
