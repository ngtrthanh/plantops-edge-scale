package twopass

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type Cycles interface {
	RecordFirstPass(context.Context, domain.WeighPass) (domain.WeighCycle, error)
	RecordSecondPass(context.Context, domain.WeighPass) (domain.WeighCycle, domain.Ticket, error)
	ResolveCalled(context.Context, string, string) (domain.WeighCycle, bool, error)
}

type CommitOutcome struct {
	PassTransactionID string
	Direction domain.Direction
	Cycle domain.WeighCycle
	FinalTicket domain.Ticket
	Err string
}

type CommitBridge struct {
	Cycles Cycles
	mu sync.RWMutex
	direction domain.Direction
	outcomes map[string]CommitOutcome
	evidence map[string][]domain.CameraEvidence
}

func NewCommitBridge(cycles Cycles)*CommitBridge{return &CommitBridge{Cycles:cycles,outcomes:make(map[string]CommitOutcome),evidence:make(map[string][]domain.CameraEvidence)}}
func (b *CommitBridge) SetDirection(direction domain.Direction){b.mu.Lock();b.direction=direction;b.mu.Unlock()}
func (b *CommitBridge) Direction()domain.Direction{b.mu.RLock();defer b.mu.RUnlock();return b.direction}

func (b *CommitBridge) AddEvidence(transactionID string,e domain.CameraEvidence)error{
	if transactionID==""{return errors.New("camera evidence requires active physical transaction")}
	if e.CameraID==""||e.ImageRef==""{return errors.New("camera evidence requires camera_id and image_ref")}
	b.mu.Lock();defer b.mu.Unlock();b.evidence[transactionID]=append(b.evidence[transactionID],e);return nil
}
func (b *CommitBridge) Evidence(transactionID string)[]domain.CameraEvidence{b.mu.RLock();defer b.mu.RUnlock();return append([]domain.CameraEvidence(nil),b.evidence[transactionID]...)}

func (b *CommitBridge) Commit(ctx context.Context,legacy domain.Ticket)error{
	if b.Cycles==nil{return errors.New("two-pass cycle coordinator unavailable")}
	b.mu.RLock();direction:=b.direction;camera:=append([]domain.CameraEvidence(nil),b.evidence[legacy.TransactionID]...);b.mu.RUnlock()
	if direction!=domain.DirectionAToB&&direction!=domain.DirectionBToA{return errors.New("physical pass direction is not established")}
	pass:=domain.WeighPass{ID:legacy.ID,Direction:direction,StationID:legacy.StationID,Plate:legacy.Plate,RFID:legacy.RFID,Weight:domain.WeightAcceptance{WeightKG:legacy.WeightKG,ObservedAt:legacy.WeightObservedAt,RawRef:legacy.WeightRawRef},Mode:legacy.Mode,Overrides:append([]domain.Override(nil),legacy.Overrides...),CameraEvidence:camera,CommittedAt:legacy.CommittedAt}
	out:=CommitOutcome{PassTransactionID:legacy.TransactionID,Direction:direction};var err error
	if direction==domain.DirectionAToB{out.Cycle,err=b.Cycles.RecordFirstPass(ctx,pass)}else{out.Cycle,out.FinalTicket,err=b.Cycles.RecordSecondPass(ctx,pass)}
	if err!=nil{out.Err=err.Error()}
	b.mu.Lock();b.outcomes[legacy.TransactionID]=out;b.mu.Unlock()
	if err!=nil{return fmt.Errorf("two-pass business commit blocked: %w",err)};return nil
}
func (b *CommitBridge) Outcome(transactionID string)(CommitOutcome,bool){b.mu.RLock();defer b.mu.RUnlock();out,ok:=b.outcomes[transactionID];return out,ok}
func (b *CommitBridge) Reset(transactionID string){b.mu.Lock();if transactionID!=""{delete(b.outcomes,transactionID);delete(b.evidence,transactionID)};b.direction="";b.mu.Unlock()}
