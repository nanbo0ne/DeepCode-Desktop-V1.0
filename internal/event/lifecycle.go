package event

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/evidence"
)

// Lifecycle annotates the legacy typed stream with stable turn/item/message
// identities. It also emits generic item lifecycle markers while preserving
// every existing event for older sinks.
func Lifecycle(s Sink) Sink {
	if s == nil {
		s = Discard
	}
	return &lifecycleSink{next: s, tools: map[string]string{}}
}

type lifecycleSink struct {
	mu               sync.Mutex
	next             Sink
	turnID           string
	agentItemID      string
	messageID        string
	reasoningItemID  string
	compactionItemID string
	lastAgentItemID  string
	lastMessageID    string
	answerCommitted  bool
	tools            map[string]string
}

func (s *lifecycleSink) RecordReadinessAudit(a evidence.ReadinessAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if recorder, ok := s.next.(ReadinessAuditSink); ok {
		recorder.RecordReadinessAudit(a)
	}
}

func (s *lifecycleSink) RecordRiskReviewAudit(a RiskReviewAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if recorder, ok := s.next.(RiskReviewAuditSink); ok {
		recorder.RecordRiskReviewAudit(a)
	}
}

func lifecycleID(prefix string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(raw[:])
	}
	return prefix + "-fallback"
}

func (s *lifecycleSink) startItem(e Event, typ ItemType, id string) {
	if id == "" {
		id = lifecycleID("item")
	}
	s.next.Emit(Event{Kind: ItemStarted, TurnID: s.turnID, ItemID: id, MessageID: e.MessageID, ItemType: typ, ItemStatus: ItemStatusStarted})
}

func (s *lifecycleSink) completeItem(e Event, typ ItemType, id string, failed bool) {
	if id == "" {
		return
	}
	status := ItemStatusCompleted
	if failed {
		status = ItemStatusFailed
	}
	s.next.Emit(Event{Kind: ItemCompleted, TurnID: s.turnID, ItemID: id, MessageID: e.MessageID, ItemType: typ, ItemStatus: status})
}

func (s *lifecycleSink) deltaItem(e Event, typ ItemType, id string) {
	if id == "" {
		return
	}
	e.Kind, e.TurnID, e.ItemID, e.ItemType, e.ItemStatus = ItemDelta, s.turnID, id, typ, ItemStatusStreaming
	s.next.Emit(e)
}

func (s *lifecycleSink) Emit(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e.Kind == TurnStarted {
		if s.turnID == "" {
			s.turnID = e.TurnID
			if s.turnID == "" {
				s.turnID = lifecycleID("turn")
			}
			s.tools = map[string]string{}
		}
		e.TurnID = s.turnID
		s.next.Emit(e)
		return
	}
	if e.TurnID == "" {
		e.TurnID = s.turnID
	}

	switch e.Kind {
	case Reasoning:
		if s.agentItemID == "" {
			s.agentItemID = lifecycleID("message")
			s.messageID = lifecycleID("assistant")
			e.MessageID = s.messageID
			s.startItem(e, ItemAgentMessage, s.agentItemID)
		}
		if s.reasoningItemID == "" {
			s.reasoningItemID = lifecycleID("reasoning")
			e.MessageID = s.messageID
			s.startItem(e, ItemReasoning, s.reasoningItemID)
		}
		e.ItemID, e.MessageID, e.ItemType, e.ItemStatus = s.reasoningItemID, s.messageID, ItemReasoning, ItemStatusStreaming
		s.deltaItem(e, ItemReasoning, s.reasoningItemID)
	case Text:
		if s.agentItemID == "" {
			s.agentItemID = lifecycleID("message")
			s.messageID = lifecycleID("assistant")
			e.MessageID = s.messageID
			s.startItem(e, ItemAgentMessage, s.agentItemID)
		}
		e.ItemID, e.MessageID, e.ItemType, e.ItemStatus = s.agentItemID, s.messageID, ItemAgentMessage, ItemStatusStreaming
		s.deltaItem(e, ItemAgentMessage, s.agentItemID)
	case Message:
		created := false
		if s.agentItemID == "" && (e.Text != "" || e.Reasoning != "") {
			s.agentItemID = lifecycleID("message")
			s.messageID = lifecycleID("assistant")
			e.MessageID = s.messageID
			s.startItem(e, ItemAgentMessage, s.agentItemID)
			created = true
		}
		e.ItemID, e.MessageID, e.ItemType, e.ItemStatus = s.agentItemID, s.messageID, ItemAgentMessage, ItemStatusCompleted
		if created {
			s.deltaItem(e, ItemAgentMessage, s.agentItemID)
		}
		s.completeItem(e, ItemReasoning, s.reasoningItemID, false)
		s.completeItem(e, ItemAgentMessage, s.agentItemID, false)
		s.lastAgentItemID, s.lastMessageID = s.agentItemID, s.messageID
		s.agentItemID, s.messageID, s.reasoningItemID = "", "", ""
	case ToolDispatch:
		id := s.tools[e.Tool.ID]
		if id == "" {
			id = lifecycleID("tool")
			s.tools[e.Tool.ID] = id
			s.startItem(e, ItemTool, id)
		}
		e.ItemID, e.ItemType, e.ItemStatus = id, ItemTool, ItemStatusStarted
	case ToolProgress:
		e.ItemID, e.ItemType, e.ItemStatus = s.tools[e.Tool.ID], ItemTool, ItemStatusStreaming
		s.deltaItem(e, ItemTool, e.ItemID)
	case ToolResult:
		id := s.tools[e.Tool.ID]
		e.ItemID, e.ItemType = id, ItemTool
		if e.Tool.Err != "" {
			e.ItemStatus = ItemStatusFailed
		} else {
			e.ItemStatus = ItemStatusCompleted
		}
		s.completeItem(e, ItemTool, id, e.Tool.Err != "")
	case Notice:
		e.ItemID, e.ItemType, e.ItemStatus = lifecycleID("notice"), ItemNotice, ItemStatusCompleted
	case Phase:
		e.ItemID, e.ItemType, e.ItemStatus = lifecycleID("phase"), ItemPlan, ItemStatusCompleted
	case CompactionStarted:
		s.compactionItemID = lifecycleID("compaction")
		e.ItemID, e.ItemType, e.ItemStatus = s.compactionItemID, ItemCompaction, ItemStatusStarted
		s.startItem(e, ItemCompaction, s.compactionItemID)
	case CompactionDone:
		e.ItemID, e.ItemType, e.ItemStatus = s.compactionItemID, ItemCompaction, ItemStatusCompleted
		s.completeItem(e, ItemCompaction, s.compactionItemID, false)
		s.compactionItemID = ""
	case AnswerCommitted:
		e.ItemID, e.MessageID = s.lastAgentItemID, s.lastMessageID
		e.FinalItemID, e.FinalMessageID = s.lastAgentItemID, s.lastMessageID
		e.ItemType, e.ItemStatus = ItemAgentMessage, ItemStatusCompleted
		s.answerCommitted = e.FinalItemID != "" && e.FinalMessageID != ""
	case TurnDone:
		if e.Outcome == "" {
			if e.Err != nil {
				e.Outcome = TurnOutcomeFailed
			} else {
				e.Outcome = TurnOutcomeSuccess
			}
		}
		if s.answerCommitted {
			e.FinalItemID, e.FinalMessageID = s.lastAgentItemID, s.lastMessageID
		} else if e.Outcome == TurnOutcomeSuccess && e.TurnID != "" {
			// A successful turn is only canonical once a visible final answer has
			// passed readiness and been committed by the agent.
			e.Outcome = TurnOutcomeInterrupted
			e.FinalItemID, e.FinalMessageID = "", ""
		}
	}

	s.next.Emit(e)
	if e.Kind == TurnDone {
		s.turnID, s.agentItemID, s.messageID, s.reasoningItemID = "", "", "", ""
		s.lastAgentItemID, s.lastMessageID, s.compactionItemID = "", "", ""
		s.answerCommitted = false
		s.tools = map[string]string{}
	}
}
