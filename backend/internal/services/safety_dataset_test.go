package services

import (
	"testing"

	"github.com/kids-trainer/backend/internal/models"
)

func TestSafetyDatasetIsEmbedded(t *testing.T) {
	if len(safetyDataset) != 67 {
		t.Fatalf("expected 67 dataset entries, got %d", len(safetyDataset))
	}
	entry, ok := datasetEntry(models.SafetyTask{Description: safetyDataset[0].Situation})
	if !ok || entry.ID != 1 {
		t.Fatal("dataset entry was not matched by situation")
	}
}
