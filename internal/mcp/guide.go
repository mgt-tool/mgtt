// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

// The guide topics are short, self-contained notes on writing models: the
// lessons a client would otherwise learn by getting a model wrong.
//
//go:embed guide/*.md
var guideFS embed.FS

// GuideParams names a topic; empty is the index.
type GuideParams struct {
	Topic string `json:"topic,omitempty"`
}

// GuideResult is one topic's text and the topics there are.
type GuideResult struct {
	Topic  string   `json:"topic"`
	Text   string   `json:"text"`
	Topics []string `json:"topics"`
}

// Guide returns a guide topic.
func (h *Handler) Guide(p GuideParams) (*GuideResult, error) {
	topics, err := guideTopics()
	if err != nil {
		return nil, err
	}
	topic := p.Topic
	if topic == "" {
		topic = "index"
	}
	data, err := guideFS.ReadFile("guide/" + topic + ".md")
	if err != nil {
		return nil, fmt.Errorf("no guide topic %q (topics: %s)", topic, strings.Join(topics, ", "))
	}
	return &GuideResult{Topic: topic, Text: string(data), Topics: topics}, nil
}

func guideTopics() ([]string, error) {
	entries, err := guideFS.ReadDir("guide")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(out)
	return out, nil
}
