package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/auditjournal"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/ingress"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/modbustcp"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/rawjournal"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/registry"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/scaleascii"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/sqlitestore"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/engine"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/httpapi"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/runtimeio"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/winservice"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/workflowaudit"
)

var (
	version = "dev"
	gitSHA  = "dev"
)

type config struct {
	listen                 string
	stationID              string
	dbPath                 string
	scaleAddr              string
	rawWeightJournal       string
	eventJournal           string
	logFile                string
	reconnectDelay         time.Duration
	ioAddr                 string
	ioUnitID               uint
	ioMapSpec              string
	ioPoll                 time.Duration
	ioTimeout              time.Duration
	buzzerPulse            time.Duration
	barrierFeedbackTimeout time.Duration
	vehicleMap             string
	emptyScaleMaxKG        int64
	minStableWeightKG      int64
	stableConfirmations    int
	stableToleranceKG      int64
	simulation             bool
}

func main() {
	var cfg config
	var serviceAction string
	flag.StringVar(&cfg.listen,"listen","127.0.0.1:8080","HTTP listen address")
	flag.StringVar(&cfg.stationID,"station-id","EDGE-01","stable station identifier used in audit records")
	flag.StringVar(&cfg.dbPath,"db","data/edge.db","SQLite edge database path")
	flag.StringVar(&cfg.scaleAddr,"scale-addr","","scale controller TCP address host:port; empty disables live collector")
	flag.StringVar(&cfg.rawWeightJournal,"raw-weight-journal","data/raw-weight.jsonl","append-only raw weight audit journal path")
	flag.StringVar(&cfg.eventJournal,"event-journal","data/events.jsonl","append-only operational event audit journal path")
	flag.StringVar(&cfg.logFile,"log-file","data/edge.log","application log file; empty disables file logging")
	flag.DurationVar(&cfg.reconnectDelay,"scale-reconnect-delay",2*time.Second,"delay before reconnecting to scale controller")
	flag.StringVar(&cfg.ioAddr,"io-addr","","Modbus TCP remote I/O host:port; empty disables hardware I/O")
	flag.UintVar(&cfg.ioUnitID,"io-unit-id",1,"Modbus TCP unit identifier 0..255")
	flag.StringVar(&cfg.ioMapSpec,"io-map","","comma-separated logical I/O address overrides; defaults follow docs/HARDWARE-WIRING.md")
	flag.DurationVar(&cfg.ioPoll,"io-poll",100*time.Millisecond,"remote I/O input/reconcile interval")
	flag.DurationVar(&cfg.ioTimeout,"io-timeout",time.Second,"Modbus TCP request timeout")
	flag.DurationVar(&cfg.buzzerPulse,"io-buzzer-pulse",700*time.Millisecond,"bounded buzzer pulse on release authorization")
	flag.DurationVar(&cfg.barrierFeedbackTimeout,"barrier-feedback-timeout",5*time.Second,"maximum wait for barrier OPEN feedback after an open request")
	flag.StringVar(&cfg.vehicleMap,"vehicle-map","","bootstrap RFID=PLATE pairs separated by commas")
	flag.Int64Var(&cfg.emptyScaleMaxKG,"empty-scale-max-kg",500,"maximum absolute stable weight treated as empty deck for entry")
	flag.Int64Var(&cfg.minStableWeightKG,"min-stable-weight-kg",1000,"minimum stable weight eligible for ticket acceptance")
	flag.IntVar(&cfg.stableConfirmations,"stable-confirmations",2,"consecutive authoritative stable frames required")
	flag.Int64Var(&cfg.stableToleranceKG,"stable-tolerance-kg",20,"maximum delta between stable confirmation frames")
	flag.BoolVar(&cfg.simulation,"simulation",false,"enable explicit /sim/* test ingress endpoints")
	flag.StringVar(&serviceAction,"service","","Windows Service action: install|start|stop|status|uninstall")
	flag.Parse()

	if cfg.ioUnitID > 255 { log.Fatal("io-unit-id must be 0..255") }
	if serviceAction != "" {
		if err := manageService(serviceAction); err != nil { log.Fatal(err) }
		return
	}

	runner := func(ctx context.Context) error { return run(ctx,cfg) }
	isService, err := winservice.IsService()
	if err != nil { log.Fatalf("detect Windows Service context: %v",err) }
	if isService {
		if err := winservice.Run(runner); err != nil { log.Fatal(err) }
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
	defer stop()
	if err := runner(ctx); err != nil { log.Fatal(err) }
}

func manageService(action string) error {
	switch action {
	case "install":
		if err:=winservice.Install(winservice.StripManagementArg(os.Args[1:]));err!=nil{return err}
		fmt.Printf("installed %s\n",winservice.Name);return nil
	case "start":
		if err:=winservice.Start();err!=nil{return err};fmt.Printf("started %s\n",winservice.Name);return nil
	case "stop":
		if err:=winservice.Stop(30*time.Second);err!=nil{return err};fmt.Printf("stopped %s\n",winservice.Name);return nil
	case "status":
		st,err:=winservice.Status();if err!=nil{return err};fmt.Println(st);return nil
	case "uninstall":
		if err:=winservice.Uninstall();err!=nil{return err};fmt.Printf("uninstalled %s\n",winservice.Name);return nil
	default:
		return fmt.Errorf("unknown -service action %q; use install|start|stop|status|uninstall",action)
	}
}

func run(ctx context.Context, cfg config) error {
	logCloser, err := configureLogging(cfg.logFile)
	if err != nil { return fmt.Errorf("log file: %w",err) }
	if logCloser != nil { defer logCloser.Close() }
	log.Printf("starting plantops-edge-scale version=%s sha=%s station=%s",version,gitSHA,cfg.stationID)

	weightAudit:=&rawjournal.Journal{Path:cfg.rawWeightJournal}
	if err:=weightAudit.Verify();err!=nil&&!os.IsNotExist(err){return fmt.Errorf("raw weight audit integrity: %w",err)}
	eventAudit:=&auditjournal.Journal{Path:cfg.eventJournal}
	if err:=eventAudit.Verify();err!=nil&&!os.IsNotExist(err){return fmt.Errorf("operational event audit integrity: %w",err)}

	store,err:=sqlitestore.Open(cfg.dbPath);if err!=nil{return fmt.Errorf("SQLite edge store: %w",err)}
	defer store.Close()
	if st,err:=store.Status(ctx);err!=nil{return fmt.Errorf("SQLite edge store integrity: %w",err)}else{log.Printf("SQLite ready path=%s schema=%d tickets=%d pending_sync=%d",st.Path,st.Schema,st.Tickets,st.PendingSync)}

	vehicleRegistry,err:=registry.Parse(cfg.vehicleMap);if err!=nil{return fmt.Errorf("vehicle map: %w",err)}
	coreWorkflow:=engine.New(engine.Config{StationID:cfg.stationID,EmptyScaleMaxKG:cfg.emptyScaleMaxKG,MinStableWeightKG:cfg.minStableWeightKG,StableConfirmations:cfg.stableConfirmations,StableToleranceKG:cfg.stableToleranceKG},store,vehicleRegistry)
	workflow:=&workflowaudit.Recorder{Engine:coreWorkflow,Audit:eventAudit,StationID:cfg.stationID,RuntimeGitSHA:gitSHA}

	rfid:=&ingress.RFID{};lpr:=&ingress.LPR{}
	rfid.OnObservation=func(o domain.RFIDObservation){if err:=workflow.ObserveRFID(context.Background(),o);err!=nil{log.Printf("workflow RFID observation: %v",err)}}
	lpr.OnObservation=func(o domain.LPRObservation){if err:=workflow.ObserveLPR(context.Background(),o);err!=nil{log.Printf("workflow LPR observation: %v",err)}}

	scaleMonitor:=scaleascii.NewMonitor(cfg.scaleAddr!="",cfg.scaleAddr)
	if cfg.scaleAddr!=""{
		collector:=&scaleascii.StreamCollector{Addr:cfg.scaleAddr,StationID:cfg.stationID,TransactionID:workflow.ActiveTransactionID,Journal:weightAudit,ReconnectDelay:cfg.reconnectDelay,
			OnReading:func(a domain.AuditedScaleReading){scaleMonitor.Reading(a.Reading);_ = workflow.ClearFault(context.Background(),domain.DeviceScale);if err:=workflow.ObserveScale(context.Background(),a);err!=nil{log.Printf("workflow audited scale observation: %v",err)}},
			OnFault:func(err error){scaleMonitor.Fault(err);_ = workflow.ObserveFault(context.Background(),domain.Fault{Device:domain.DeviceScale,Health:domain.HealthFault,Reason:err.Error(),Overridable:false,Critical:true})},}
		go func(){if err:=collector.Run(ctx);err!=nil&&ctx.Err()==nil{scaleMonitor.Fault(err);_ = workflow.ObserveFault(context.Background(),domain.Fault{Device:domain.DeviceScale,Health:domain.HealthFault,Reason:"raw weight collector stopped: "+err.Error(),Overridable:false,Critical:true});log.Printf("CRITICAL raw weight collector stopped: %v",err)}}()
		log.Printf("raw weight collector enabled station=%s scale=%s journal=%s",cfg.stationID,cfg.scaleAddr,cfg.rawWeightJournal)
	}else{log.Printf("raw weight collector disabled: entry authorization cannot pass empty-scale proof until live audited scale is configured")}

	ioMonitor:=runtimeio.NewMonitor(false)
	if cfg.ioAddr!=""{
		mapping,err:=modbustcp.ParseMapping(cfg.ioMapSpec);if err!=nil{return fmt.Errorf("io-map: %w",err)}
		if mapping.SafetyClear==nil{log.Printf("WARNING remote I/O enabled without safety_clear mapping; fail-safe SafetyClear=false will block automatic entry")}
		client:=&modbustcp.Client{Addr:cfg.ioAddr,UnitID:byte(cfg.ioUnitID),Timeout:cfg.ioTimeout};hardwareIO:=&modbustcp.IO{Client:client,Mapping:mapping}
		auditedOutputs:=&runtimeio.AuditedOutputs{Inner:hardwareIO,Audit:eventAudit,StationID:cfg.stationID,RuntimeGitSHA:gitSHA,TransactionID:workflow.ActiveTransactionID,
			OnAuditFailure:func(err error){_ = coreWorkflow.ObserveFault(context.Background(),domain.Fault{Device:domain.DeviceAuditStore,Health:domain.HealthFault,Reason:"operational audit output gate failed: "+err.Error(),Overridable:false,Critical:true})}}
		ioMonitor=runtimeio.NewMonitor(true);controller:=&runtimeio.Controller{Workflow:workflow,Inputs:hardwareIO,Outputs:auditedOutputs,Monitor:ioMonitor,PollInterval:cfg.ioPoll,BuzzerPulse:cfg.buzzerPulse,BarrierFeedbackTimeout:cfg.barrierFeedbackTimeout}
		go func(){if err:=controller.Run(ctx);err!=nil&&ctx.Err()==nil{log.Printf("CRITICAL runtime I/O controller stopped: %v",err)}}();log.Printf("remote I/O enabled addr=%s unit=%d %s",cfg.ioAddr,cfg.ioUnitID,controller.String())
	}else{log.Printf("remote I/O disabled: no physical sensor/output commands will be issued")}

	s:=&httpapi.Server{RFID:rfid,LPR:lpr,WeightAudit:weightAudit,EventAudit:eventAudit,ScaleMonitor:scaleMonitor,Workflow:workflow,
		IOStatus:func()any{return ioMonitor.Snapshot()},StorageStatus:func(c context.Context)(any,error){return store.Status(c)},AllowSimulation:cfg.simulation,Version:version,GitSHA:gitSHA}
	httpServer:=&http.Server{Addr:cfg.listen,Handler:s.Handler(),ReadHeaderTimeout:5*time.Second}
	httpErr:=make(chan error,1)
	go func(){
		log.Printf("plantops-edge-scale ready http=%s simulation=%v db=%s",cfg.listen,cfg.simulation,cfg.dbPath)
		err:=httpServer.ListenAndServe()
		if err!=nil&&!errors.Is(err,http.ErrServerClosed){httpErr<-err;return}
		httpErr<-nil
	}()

	select{
	case err:=<-httpErr:
		if err!=nil{return fmt.Errorf("HTTP server: %w",err)}
		return nil
	case <-ctx.Done():
		log.Printf("shutdown requested")
		shutdownCtx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
		defer cancel()
		if err:=httpServer.Shutdown(shutdownCtx);err!=nil{log.Printf("HTTP shutdown: %v",err)}
		if err:=store.Checkpoint(shutdownCtx);err!=nil{log.Printf("SQLite checkpoint on shutdown: %v",err)}
		log.Printf("shutdown complete")
		return nil
	}
}

func configureLogging(path string)(io.Closer,error){
	if path==""{return nil,nil}
	dir:=filepath.Dir(path);if dir!="."&&dir!=""{if err:=os.MkdirAll(dir,0o755);err!=nil{return nil,err}}
	f,err:=os.OpenFile(path,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0o644);if err!=nil{return nil,err}
	log.SetOutput(io.MultiWriter(os.Stdout,f));log.SetFlags(log.LstdFlags|log.LUTC|log.Lmicroseconds)
	return f,nil
}
