package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/auditjournal"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/ingress"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/modbustcp"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/rawjournal"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/registry"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/scaleascii"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/sqlitestore"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/cycle"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/engine"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/httpapi"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/runtimeio"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/twopass"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/workflowaudit"
)

var ( version="dev"; gitSHA="dev" )

func main(){
	listen:=flag.String("listen","127.0.0.1:8080","HTTP listen address")
	stationID:=flag.String("station-id","EDGE-01","stable station identifier used in audit records")
	dbPath:=flag.String("db","data/edge.db","SQLite edge database path")
	scaleAddr:=flag.String("scale-addr","","scale controller TCP address host:port; empty disables live collector")
	rawWeightJournal:=flag.String("raw-weight-journal","data/raw-weight.jsonl","append-only raw weight audit journal path")
	eventJournal:=flag.String("event-journal","data/events.jsonl","append-only operational event audit journal path")
	reconnectDelay:=flag.Duration("scale-reconnect-delay",2*time.Second,"delay before reconnecting to scale controller")
	ioAddr:=flag.String("io-addr","","Modbus TCP remote I/O host:port; empty disables hardware I/O")
	ioUnitID:=flag.Uint("io-unit-id",1,"Modbus TCP unit identifier 0..255")
	ioMapSpec:=flag.String("io-map","","comma-separated logical I/O address overrides; defaults follow docs/HARDWARE-WIRING.md")
	ioPoll:=flag.Duration("io-poll",100*time.Millisecond,"remote I/O input/reconcile interval")
	ioTimeout:=flag.Duration("io-timeout",time.Second,"Modbus TCP request timeout")
	buzzerPulse:=flag.Duration("io-buzzer-pulse",700*time.Millisecond,"bounded buzzer pulse on release authorization")
	barrierFeedbackTimeout:=flag.Duration("barrier-feedback-timeout",5*time.Second,"maximum wait for barrier OPEN feedback after an open request")
	vehicleMap:=flag.String("vehicle-map","","bootstrap RFID=PLATE pairs separated by commas")
	emptyScaleMaxKG:=flag.Int64("empty-scale-max-kg",500,"maximum absolute stable weight treated as empty deck for entry")
	minStableWeightKG:=flag.Int64("min-stable-weight-kg",1000,"minimum stable weight eligible for pass acceptance")
	stableConfirmations:=flag.Int("stable-confirmations",2,"consecutive authoritative stable frames required")
	stableToleranceKG:=flag.Int64("stable-tolerance-kg",20,"maximum delta between stable confirmation frames")

	// Pair policy is business/master-data configuration. Zero means the limit is
	// not configured; no site-specific threshold is hidden in code.
	pairMinElapsed:=flag.Duration("pair-min-elapsed",0,"minimum valid elapsed time between A->B and B->A pass; 0 disables limit")
	pairMaxElapsed:=flag.Duration("pair-max-elapsed",0,"maximum valid elapsed time between A->B and B->A pass; 0 disables expiry")
	pairMinGrossKG:=flag.Int64("pair-min-gross-kg",0,"minimum valid first/gross weight; 0 disables limit")
	pairMaxGrossKG:=flag.Int64("pair-max-gross-kg",0,"maximum valid first/gross weight; 0 disables limit")
	pairMinTareKG:=flag.Int64("pair-min-tare-kg",0,"minimum valid second/tare weight; 0 disables limit")
	pairMaxTareKG:=flag.Int64("pair-max-tare-kg",0,"maximum valid second/tare weight; 0 disables limit")
	pairMinNetKG:=flag.Int64("pair-min-net-kg",0,"minimum valid gross-tare net weight; 0 disables limit")
	pairMaxNetKG:=flag.Int64("pair-max-net-kg",0,"maximum valid gross-tare net weight; 0 disables limit")

	simulation:=flag.Bool("simulation",false,"enable explicit /sim/* test ingress endpoints")
	flag.Parse()
	if *ioUnitID>255{log.Fatalf("io-unit-id must be 0..255")}
	if *pairMinElapsed<0||*pairMaxElapsed<0{log.Fatalf("pair elapsed limits cannot be negative")}
	if *pairMaxElapsed>0&&*pairMinElapsed>*pairMaxElapsed{log.Fatalf("pair-min-elapsed cannot exceed pair-max-elapsed")}

	weightAudit:=&rawjournal.Journal{Path:*rawWeightJournal}
	if err:=weightAudit.Verify();err!=nil&&!os.IsNotExist(err){log.Fatalf("raw weight audit integrity check failed: %v",err)}
	eventAudit:=&auditjournal.Journal{Path:*eventJournal}
	if err:=eventAudit.Verify();err!=nil&&!os.IsNotExist(err){log.Fatalf("operational event audit integrity check failed: %v",err)}

	store,err:=sqlitestore.Open(*dbPath);if err!=nil{log.Fatalf("SQLite edge store: %v",err)}
	defer store.Close()
	if st,err:=store.Status(context.Background());err!=nil{log.Fatalf("SQLite edge store integrity: %v",err)}else{log.Printf("SQLite ready path=%s schema=%d tickets=%d queued=%d called=%d completed_cycles=%d pending_sync=%d",st.Path,st.Schema,st.Tickets,st.QueuedCycles,st.CalledCycles,st.CompletedCycles,st.PendingSync)}

	pairPolicy:=domain.PairPolicy{
		MinElapsed:*pairMinElapsed,MaxElapsed:*pairMaxElapsed,
		MinGrossKG:*pairMinGrossKG,MaxGrossKG:*pairMaxGrossKG,
		MinTareKG:*pairMinTareKG,MaxTareKG:*pairMaxTareKG,
		MinNetKG:*pairMinNetKG,MaxNetKG:*pairMaxNetKG,
	}
	cycleCoordinator:=cycle.New(store,pairPolicy)
	commitBridge:=twopass.NewCommitBridge(cycleCoordinator)

	vehicleRegistry,err:=registry.Parse(*vehicleMap);if err!=nil{log.Fatalf("vehicle map: %v",err)}
	innerWorkflow:=engine.New(engine.Config{StationID:*stationID,EmptyScaleMaxKG:*emptyScaleMaxKG,MinStableWeightKG:*minStableWeightKG,StableConfirmations:*stableConfirmations,StableToleranceKG:*stableToleranceKG},commitBridge,vehicleRegistry)
	twoWayWorkflow:=twopass.NewWorkflow(innerWorkflow,commitBridge,cycleCoordinator)
	workflow:=&workflowaudit.Recorder{Engine:twoWayWorkflow,Audit:eventAudit,StationID:*stationID,RuntimeGitSHA:gitSHA}

	// Pair expiry is business housekeeping only. It never creates a ticket and
	// never blocks the physical scale. A zero MaxElapsed means no automatic
	// expiry until site master data is configured.
	if pairPolicy.MaxElapsed>0{
		go func(){ticker:=time.NewTicker(time.Minute);defer ticker.Stop();for range ticker.C{n,err:=cycleCoordinator.ExpireOrphans(context.Background(),time.Now().UTC());if err!=nil{log.Printf("cycle orphan expiry: %v",err)}else if n>0{log.Printf("cycle orphan expiry marked %d incomplete cycle(s)",n)}}}()
	}

	rfid:=&ingress.RFID{};lpr:=&ingress.LPR{}
	rfid.OnObservation=func(o domain.RFIDObservation){if err:=workflow.ObserveRFID(context.Background(),o);err!=nil{log.Printf("workflow RFID observation: %v",err)}}
	lpr.OnObservation=func(o domain.LPRObservation){if err:=workflow.ObserveLPR(context.Background(),o);err!=nil{log.Printf("workflow LPR observation: %v",err)}}

	scaleMonitor:=scaleascii.NewMonitor(*scaleAddr!="",*scaleAddr)
	if *scaleAddr!=""{
		collector:=&scaleascii.StreamCollector{Addr:*scaleAddr,StationID:*stationID,TransactionID:workflow.ActiveTransactionID,Journal:weightAudit,ReconnectDelay:*reconnectDelay,
			OnReading:func(a domain.AuditedScaleReading){scaleMonitor.Reading(a.Reading);_ = workflow.ClearFault(context.Background(),domain.DeviceScale);if err:=workflow.ObserveScale(context.Background(),a);err!=nil{log.Printf("workflow audited scale observation: %v",err)}},
			OnFault:func(err error){scaleMonitor.Fault(err);_ = workflow.ObserveFault(context.Background(),domain.Fault{Device:domain.DeviceScale,Health:domain.HealthFault,Reason:err.Error(),Overridable:false,Critical:true})},}
		go func(){if err:=collector.Run(context.Background());err!=nil{scaleMonitor.Fault(err);_ = workflow.ObserveFault(context.Background(),domain.Fault{Device:domain.DeviceScale,Health:domain.HealthFault,Reason:"raw weight collector stopped: "+err.Error(),Overridable:false,Critical:true});log.Printf("CRITICAL raw weight collector stopped: %v",err)}}()
		log.Printf("raw weight collector enabled station=%s scale=%s journal=%s",*stationID,*scaleAddr,*rawWeightJournal)
	}else{log.Printf("raw weight collector disabled: entry authorization cannot pass empty-scale proof until live audited scale is configured")}

	ioMonitor:=runtimeio.NewMonitor(false)
	if *ioAddr!=""{
		mapping,err:=modbustcp.ParseMapping(*ioMapSpec);if err!=nil{log.Fatalf("io-map: %v",err)}
		if mapping.SafetyClear==nil{log.Printf("WARNING remote I/O enabled without safety_clear mapping; fail-safe SafetyClear=false will block automatic entry")}
		client:=&modbustcp.Client{Addr:*ioAddr,UnitID:byte(*ioUnitID),Timeout:*ioTimeout};hardwareIO:=&modbustcp.IO{Client:client,Mapping:mapping}
		auditedOutputs:=&runtimeio.AuditedOutputs{Inner:hardwareIO,Audit:eventAudit,StationID:*stationID,RuntimeGitSHA:gitSHA,TransactionID:workflow.ActiveTransactionID,
			OnAuditFailure:func(err error){_ = workflow.ObserveFault(context.Background(),domain.Fault{Device:domain.DeviceAuditStore,Health:domain.HealthFault,Reason:"operational audit output gate failed: "+err.Error(),Overridable:false,Critical:true})}}
		ioMonitor=runtimeio.NewMonitor(true);controller:=&runtimeio.Controller{Workflow:workflow,Inputs:hardwareIO,Outputs:auditedOutputs,Monitor:ioMonitor,PollInterval:*ioPoll,BuzzerPulse:*buzzerPulse,BarrierFeedbackTimeout:*barrierFeedbackTimeout}
		go func(){if err:=controller.Run(context.Background());err!=nil{log.Printf("CRITICAL runtime I/O controller stopped: %v",err)}}();log.Printf("remote I/O enabled addr=%s unit=%d %s",*ioAddr,*ioUnitID,controller.String())
	}else{log.Printf("remote I/O disabled: no physical sensor/output commands will be issued")}

	s:=&httpapi.Server{RFID:rfid,LPR:lpr,WeightAudit:weightAudit,EventAudit:eventAudit,ScaleMonitor:scaleMonitor,Workflow:workflow,Cycles:cycleCoordinator,
		IOStatus:func()any{return ioMonitor.Snapshot()},StorageStatus:func(ctx context.Context)(any,error){return store.Status(ctx)},AllowSimulation:*simulation,Version:version,GitSHA:gitSHA}
	httpServer:=&http.Server{Addr:*listen,Handler:s.Handler(),ReadHeaderTimeout:5*time.Second}
	log.Printf("plantops-edge-scale %s sha=%s http=%s simulation=%v db=%s two_pass=true",version,gitSHA,*listen,*simulation,*dbPath)
	log.Fatal(httpServer.ListenAndServe())
}
