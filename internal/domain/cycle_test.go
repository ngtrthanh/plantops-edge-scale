package domain

import (
	"testing"
	"time"
)

func TestValidatePairHappyPath(t *testing.T) {
	t0 := time.Date(2026, 8, 21, 1, 0, 0, 0, time.UTC)
	first := WeighPass{
		Number: PassFirst, Direction: DirectionAToB, Plate: "15C-123.45", RFID: "RFID-1",
		Weight: WeightAcceptance{WeightKG: 28460, ObservedAt: t0, RawRef: RawWeightRef{Seq: 10, Hash: "a"}},
	}
	second := WeighPass{
		Number: PassSecond, Direction: DirectionBToA, Plate: "15C12345", RFID: "RFID-1",
		Weight: WeightAcceptance{WeightKG: 11820, ObservedAt: t0.Add(45 * time.Minute), RawRef: RawWeightRef{Seq: 90, Hash: "b"}},
	}
	got := ValidatePair(first, second, PairPolicy{MaxElapsed: 4 * time.Hour, MinNetKG: 1000})
	if !got.Valid || got.Status != CycleComplete {
		t.Fatalf("expected valid pair, got %+v", got)
	}
	if got.NetKG != 16640 {
		t.Fatalf("expected net 16640, got %d", got.NetKG)
	}
}

func TestValidatePairRejectsWrongDirection(t *testing.T) {
	t0 := time.Now().UTC()
	first := passForTest(PassFirst, DirectionBToA, "15C-1", 20000, t0, 1)
	second := passForTest(PassSecond, DirectionAToB, "15C-1", 10000, t0.Add(time.Hour), 2)
	got := ValidatePair(first, second, PairPolicy{})
	if got.Valid {
		t.Fatal("wrong-direction pair must not validate")
	}
}

func TestValidatePairRejectsWrongTruck(t *testing.T) {
	t0 := time.Now().UTC()
	first := passForTest(PassFirst, DirectionAToB, "15C-111.11", 20000, t0, 1)
	second := passForTest(PassSecond, DirectionBToA, "15C-222.22", 10000, t0.Add(time.Hour), 2)
	got := ValidatePair(first, second, PairPolicy{})
	if got.Status != CycleWrongTruck {
		t.Fatalf("expected WRONG_TRUCK, got %+v", got)
	}
}

func TestValidatePairRejectsExpiredPair(t *testing.T) {
	t0 := time.Now().UTC()
	first := passForTest(PassFirst, DirectionAToB, "15C-1", 20000, t0, 1)
	second := passForTest(PassSecond, DirectionBToA, "15C-1", 10000, t0.Add(7*time.Hour), 2)
	got := ValidatePair(first, second, PairPolicy{MaxElapsed: 6 * time.Hour})
	if got.Status != CyclePairTimeInvalid {
		t.Fatalf("expected PAIR_TIME_INVALID, got %+v", got)
	}
}

func TestValidatePairRejectsNonPositiveNet(t *testing.T) {
	t0 := time.Now().UTC()
	first := passForTest(PassFirst, DirectionAToB, "15C-1", 10000, t0, 1)
	second := passForTest(PassSecond, DirectionBToA, "15C-1", 11000, t0.Add(time.Hour), 2)
	got := ValidatePair(first, second, PairPolicy{})
	if got.Status != CyclePairWeightInvalid {
		t.Fatalf("expected PAIR_WEIGHT_INVALID, got %+v", got)
	}
}

func TestValidatePairRequiresRawRefs(t *testing.T) {
	t0 := time.Now().UTC()
	first := passForTest(PassFirst, DirectionAToB, "15C-1", 20000, t0, 0)
	second := passForTest(PassSecond, DirectionBToA, "15C-1", 10000, t0.Add(time.Hour), 2)
	got := ValidatePair(first, second, PairPolicy{})
	if got.Valid {
		t.Fatal("pair without first raw audit ref must not validate")
	}
}

func passForTest(n PassNumber, d Direction, plate string, weight int64, at time.Time, seq uint64) WeighPass {
	hash := ""
	if seq != 0 {
		hash = "hash"
	}
	return WeighPass{
		Number: n,
		Direction: d,
		Plate: plate,
		Weight: WeightAcceptance{WeightKG: weight, ObservedAt: at, RawRef: RawWeightRef{Seq: seq, Hash: hash}},
	}
}
