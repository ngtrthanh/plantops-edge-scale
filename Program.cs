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
app.MapGet("/api/tickets", (EdgeDemo edge) => Results.Ok(edge.Tickets));

app.MapPost("/api/demo/run", (EdgeDemo edge) =>
{
    if (!edge.TryStart())
        return Results.Conflict(new { error = "cycle already running" });

    _ = edge.RunAsync();
    return Results.Accepted(value: new { status = "started" });
});

app.MapFallbackToFile("index.html");
app.Run();

public sealed class EdgeDemo
{
    private readonly object _gate = new();
    private readonly List<Ticket> _tickets = new();
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

    public bool TryStart() => Interlocked.CompareExchange(ref _running, 1, 0) == 0;

    public async Task RunAsync()
    {
        try
        {
            Set(EdgeState.Idle with { Phase = "APPROACH", Running = true, SensorEntry = true });
            await Pause();

            Set(State with
            {
                Phase = "IDENTIFIED",
                Plate = "15C-123.45",
                Rfid = "RFID-DEMO-001",
                EntryLight = "GREEN",
                EntryBarrier = "OPEN"
            });
            await Pause();

            Set(State with
            {
                Phase = "ENTERING",
                SensorFront = true,
                WeightKg = 11800,
                WeightStable = false
            });
            await Pause();

            Set(State with
            {
                Phase = "POSITIONED",
                SensorEntry = false,
                SensorFront = true,
                SensorRear = true,
                EntryLight = "RED",
                EntryBarrier = "CLOSED",
                WeightKg = 28320
            });
            await Pause();

            Set(State with { Phase = "WEIGHING", WeightKg = 28470 });
            await Pause();
            Set(State with { WeightKg = 28460, WeightStable = true });
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

            lock (_gate) _tickets.Add(ticket);
            await AppendTicketAsync(ticket);

            Set(State with
            {
                Phase = "RELEASE",
                Buzzer = true,
                ExitLight = "GREEN",
                ExitBarrier = "OPEN"
            });
            await Task.Delay(500);
            Set(State with { Buzzer = false });
            await Pause();

            Set(State with
            {
                Phase = "EXITING",
                SensorFront = false,
                SensorRear = false,
                SensorExit = true,
                WeightKg = 0,
                WeightStable = false
            });
            await Pause();

            Set(EdgeState.Idle with { Phase = "COMPLETE" });
        }
        catch (Exception ex)
        {
            Set(EdgeState.Idle with { Phase = $"FAULT: {ex.Message}" });
        }
        finally
        {
            Interlocked.Exchange(ref _running, 0);
            Set(State with { Running = false });
        }
    }

    private void Set(EdgeState next)
    {
        lock (_gate)
        {
            _state = next with { UpdatedAt = DateTimeOffset.UtcNow };
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

public sealed record Ticket(
    string TicketId,
    string Plate,
    string Rfid,
    decimal WeightKg,
    DateTimeOffset At);
