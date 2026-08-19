using System.Text.Json;

var builder = WebApplication.CreateBuilder(args);

builder.Services.AddWindowsService(options =>
{
    options.ServiceName = "PlantOps.Edge.Scale";
});

builder.WebHost.UseUrls("http://127.0.0.1:8080");
builder.Services.AddSingleton<EdgeDemo>();

var app = builder.Build();

app.UseDefaultFiles();
app.UseStaticFiles();

app.MapGet("/healthz", () => Results.Ok(new
{
    status = "ok",
    service = "plantops-edge-scale",
    git_sha = Environment.GetEnvironmentVariable("GIT_SHA") ?? "dev",
    utc = DateTimeOffset.UtcNow
}));

app.MapGet("/api/state", (EdgeDemo edge) => Results.Ok(edge.State));
app.MapGet("/api/events", (EdgeDemo edge) => Results.Ok(edge.Events));
app.MapGet("/api/tickets", (EdgeDemo edge) => Results.Ok(edge.Tickets));

app.MapPost("/api/demo/run", (EdgeDemo edge) =>
{
    if (!edge.TryStart())
        return Results.Conflict(new { error = "cycle already running" });

    _ = edge.RunAsync();
    return Results.Accepted(value: new { status = "started" });
});

app.MapPost("/api/reset", (EdgeDemo edge) =>
{
    if (!edge.TryReset())
        return Results.Conflict(new { error = "cannot reset while cycle is running" });

    return Results.Ok(new { status = "reset" });
});

app.MapFallbackToFile("index.html");
app.Run();

public sealed class EdgeDemo
{
    private readonly object _gate = new();
    private readonly List<Ticket> _tickets = new();
    private readonly List<EdgeEvent> _events = new();
    private int _running;
    private EdgeState _state = EdgeState.Idle;

    public EdgeState State
    {
        get { lock (_gate) return _state; }
    }

    public Ticket[] Tickets
    {
        get { lock (_gate) return _tickets.OrderByDescending(x => x.At).Take(20).ToArray(); }
    }

    public EdgeEvent[] Events
    {
        get { lock (_gate) return _events.OrderByDescending(x => x.At).Take(80).ToArray(); }
    }

    public bool TryStart() => Interlocked.CompareExchange(ref _running, 1, 0) == 0;

    public bool TryReset()
    {
        if (Volatile.Read(ref _running) != 0)
            return false;

        Set(EdgeState.Idle, "SYSTEM", "Demo state reset");
        return true;
    }

    public async Task RunAsync()
    {
        try
        {
            Set(EdgeState.Idle with
            {
                Phase = "APPROACH",
                Running = true,
                SensorEntry = true
            }, "POSITION", "Truck detected at entry sensor");
            await Pause();

            Set(State with
            {
                Rfid = "RFID-DEMO-001"
            }, "RFID", "RFID reader matched tag RFID-DEMO-001");
            await Pause();

            AddEvent("CAMERA", "Plate camera captured vehicle image");
            await Task.Delay(450);

            Set(State with
            {
                Plate = "15C-123.45"
            }, "OCR", "Plate recognition completed: 15C-123.45");
            await Pause();

            Set(State with
            {
                Phase = "ENTERING",
                EntryLight = "GREEN",
                EntryBarrier = "OPEN"
            }, "CONTROL", "Entry authorized: green light and entry barrier open");
            await Pause();

            Set(State with
            {
                SensorFront = true,
                WeightKg = 11800,
                WeightStable = false
            }, "SCALE", "Truck front entered scale; live weight 11,800 kg");
            await Pause();

            Set(State with
            {
                Phase = "WEIGHING",
                SensorEntry = false,
                SensorFront = true,
                SensorRear = true,
                EntryLight = "RED",
                EntryBarrier = "CLOSED",
                WeightKg = 28320
            }, "POSITION", "Front and rear sensors confirm truck fully positioned; entry locked");
            await Pause();

            Set(State with
            {
                WeightKg = 28470,
                WeightStable = false
            }, "SCALE", "Weight settling at 28,470 kg");
            await Pause();

            Set(State with
            {
                WeightKg = 28460,
                WeightStable = true
            }, "SCALE", "Stable weight accepted: 28,460 kg");
            await Pause();

            var s = State;
            if (!s.WeightStable || !s.SensorFront || !s.SensorRear || s.Plate is null || s.Rfid is null)
                throw new InvalidOperationException("interlock not satisfied");

            var ticket = new Ticket(
                $"DEMO-{DateTimeOffset.UtcNow:yyyyMMddHHmmss}",
                s.Plate,
                s.Rfid,
                s.WeightKg,
                DateTimeOffset.UtcNow);

            lock (_gate)
            {
                _tickets.Add(ticket);
            }

            await AppendTicketAsync(ticket);
            AddEvent("TICKET", $"Ticket saved locally: {ticket.TicketId}");

            Set(State with
            {
                Phase = "RELEASE",
                Buzzer = true,
                ExitLight = "GREEN",
                ExitBarrier = "OPEN"
            }, "CONTROL", "Exit authorized: buzzer on, green light, exit barrier open");
            await Task.Delay(650);

            Set(State with
            {
                Buzzer = false
            }, "BUZZER", "Release buzzer off");
            await Pause();

            Set(State with
            {
                Phase = "EXITING",
                SensorFront = false,
                SensorRear = false,
                SensorExit = true,
                WeightKg = 0,
                WeightStable = false
            }, "POSITION", "Truck leaving scale; exit sensor active");
            await Pause();

            Set(EdgeState.Idle with
            {
                Phase = "COMPLETE"
            }, "FLOW", "Truck cycle completed; barriers returned to closed safe state");
        }
        catch (Exception ex)
        {
            Set(EdgeState.Idle with
            {
                Phase = $"FAULT: {ex.Message}"
            }, "FAULT", ex.Message);
        }
        finally
        {
            Interlocked.Exchange(ref _running, 0);
            Set(State with { Running = false });
        }
    }

    private void Set(EdgeState next, string? source = null, string? message = null)
    {
        lock (_gate)
        {
            _state = next with { UpdatedAt = DateTimeOffset.UtcNow };

            if (source is not null && message is not null)
            {
                AddEventUnsafe(source, message);
            }
        }
    }

    private void AddEvent(string source, string message)
    {
        lock (_gate)
        {
            AddEventUnsafe(source, message);
        }
    }

    private void AddEventUnsafe(string source, string message)
    {
        _events.Add(new EdgeEvent(DateTimeOffset.UtcNow, source, message));

        if (_events.Count > 200)
        {
            _events.RemoveRange(0, _events.Count - 200);
        }
    }

    private static Task Pause() => Task.Delay(900);

    private static async Task AppendTicketAsync(Ticket ticket)
    {
        var dir = Path.Combine(AppContext.BaseDirectory, "data");
        Directory.CreateDirectory(dir);
        await File.AppendAllTextAsync(
            Path.Combine(dir, "tickets.jsonl"),
            JsonSerializer.Serialize(ticket) + Environment.NewLine);
    }
}

public sealed record EdgeState(
    string Phase,
    decimal WeightKg,
    bool WeightStable,
    string? Rfid,
    string? Plate,
    bool SensorEntry,
    bool SensorFront,
    bool SensorRear,
    bool SensorExit,
    string EntryLight,
    string ExitLight,
    bool Buzzer,
    string EntryBarrier,
    string ExitBarrier,
    bool Running,
    DateTimeOffset UpdatedAt)
{
    public static EdgeState Idle => new(
        "IDLE", 0, false, null, null,
        false, false, false, false,
        "RED", "RED", false,
        "CLOSED", "CLOSED", false,
        DateTimeOffset.UtcNow);
}

public sealed record EdgeEvent(
    DateTimeOffset At,
    string Source,
    string Message);

public sealed record Ticket(
    string TicketId,
    string Plate,
    string Rfid,
    decimal WeightKg,
    DateTimeOffset At);
