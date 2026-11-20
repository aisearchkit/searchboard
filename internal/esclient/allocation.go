package esclient

import (
	"context"
	"net/http"

	"github.com/searchboard/searchboard/internal/domain"
)

// AllocationExplain asks the cluster why a shard copy is (not) allocated.
// Only deciders that vetoed or throttled allocation are kept, to keep the
// result readable on large clusters. The cluster answers HTTP 400 when the
// requested shard is already assigned and no explanation is needed.
func (c *Client) AllocationExplain(ctx context.Context, req domain.AllocationExplainRequest) (domain.AllocationExplain, error) {
	var response struct {
		Index          string `json:"index"`
		Shard          int    `json:"shard"`
		Primary        bool   `json:"primary"`
		CurrentState   string `json:"current_state"`
		UnassignedInfo struct {
			Reason string `json:"reason"`
		} `json:"unassigned_info"`
		CanAllocate         string `json:"can_allocate"`
		AllocateExplanation string `json:"allocate_explanation"`
		NodeDecisions       []struct {
			NodeName     string `json:"node_name"`
			NodeDecision string `json:"node_decision"`
			Deciders     []struct {
				Decider     string `json:"decider"`
				Decision    string `json:"decision"`
				Explanation string `json:"explanation"`
			} `json:"deciders"`
		} `json:"node_allocation_decisions"`
	}

	if err := c.doJSON(ctx, http.MethodPost, "_cluster/allocation/explain", nil, req, &response); err != nil {
		return domain.AllocationExplain{}, err
	}

	result := domain.AllocationExplain{
		Index:            response.Index,
		Shard:            response.Shard,
		Primary:          response.Primary,
		CurrentState:     response.CurrentState,
		UnassignedReason: response.UnassignedInfo.Reason,
		CanAllocate:      response.CanAllocate,
		Explanation:      response.AllocateExplanation,
	}
	for _, node := range response.NodeDecisions {
		decision := domain.AllocationNodeDecision{NodeName: node.NodeName, Decision: node.NodeDecision}
		for _, decider := range node.Deciders {
			if decider.Decision == "YES" {
				continue
			}
			decision.Deciders = append(decision.Deciders, domain.AllocationDecider{
				Name:        decider.Decider,
				Decision:    decider.Decision,
				Explanation: decider.Explanation,
			})
		}
		result.NodeDecisions = append(result.NodeDecisions, decision)
	}
	return result, nil
}
