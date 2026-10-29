package services

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kids-trainer/backend/internal/models"
)

//go:embed safety_dataset.json
var safetyDatasetJSON []byte

var safetyDataset = loadSafetyDataset()

func loadSafetyDataset() []models.SafetyDatasetEntry {
	var entries []models.SafetyDatasetEntry
	if err := json.Unmarshal(safetyDatasetJSON, &entries); err != nil {
		panic(fmt.Errorf("load embedded safety dataset: %w", err))
	}
	return entries
}

func datasetEntry(task models.SafetyTask) (models.SafetyDatasetEntry, bool) {
	description := strings.TrimSpace(task.Description)
	tip := strings.TrimSpace(task.SafetyTip)
	for _, entry := range safetyDataset {
		if description != "" && description == strings.TrimSpace(entry.Situation) {
			return entry, true
		}
		if tip != "" && tip == strings.TrimSpace(entry.Solution) {
			return entry, true
		}
	}
	return models.SafetyDatasetEntry{}, false
}

func datasetExamples(category models.SafetyCategory, limit int) string {
	var parts []string
	for _, entry := range safetyDataset {
		if category != "" && !datasetCategoryMatches(category, entry.Topic) {
			continue
		}
		parts = append(parts, datasetPromptEntry(entry))
		if len(parts) == limit {
			break
		}
	}
	return strings.Join(parts, "\n\n")
}

func datasetCategoryMatches(category models.SafetyCategory, topic string) bool {
	topic = strings.ToLower(topic)
	switch category {
	case models.CategoryHome:
		return strings.Contains(topic, "дом") || strings.Contains(topic, "интернет") || strings.Contains(topic, "огн")
	case models.CategoryCountryside:
		return strings.Contains(topic, "лес") || strings.Contains(topic, "природ") || strings.Contains(topic, "живот")
	default:
		return true
	}
}

func datasetPromptEntry(entry models.SafetyDatasetEntry) string {
	return fmt.Sprintf("Тема: %s\nСитуация из базы: %s\nЭталонное решение методистов: %s", strings.TrimSpace(entry.Topic), strings.TrimSpace(entry.Situation), strings.TrimSpace(entry.Solution))
}
