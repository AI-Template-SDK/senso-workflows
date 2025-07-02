// workflows/monitoring.go
package workflows

import (
	"context"
	"fmt"

	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"
)

// Add a monitoring function to check load distribution
func (p *ScheduledProcessor) WeeklyLoadAnalyzer() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:   "weekly-load-analyzer",
			Name: "Analyze Weekly Load Distribution",
		},
		inngestgo.CronTrigger("0 0 * * 0"), // Every Sunday at midnight
		func(ctx context.Context, input inngestgo.Input[any]) (any, error) {
			// Analyze org distribution across weekdays
			distribution, err := step.Run(ctx, "get-weekday-distribution", func(ctx context.Context) (map[string]int, error) {
				return p.orgService.GetOrgCountByWeekday(ctx)
			})

			if err != nil {
				return nil, err
			}

			// Calculate load variance
			var total int
			for _, count := range distribution {
				total += count
			}
			avgPerDay := total / 7

			// Find days with high/low load
			highLoadDays := []string{}
			lowLoadDays := []string{}

			for day, count := range distribution {
				variance := float64(count-avgPerDay) / float64(avgPerDay) * 100
				if variance > 20 {
					highLoadDays = append(highLoadDays, fmt.Sprintf("%s: %d orgs (+%.1f%%)", day, count, variance))
				} else if variance < -20 {
					lowLoadDays = append(lowLoadDays, fmt.Sprintf("%s: %d orgs (%.1f%%)", day, count, variance))
				}
			}

			return map[string]interface{}{
				"total_orgs":       total,
				"avg_orgs_per_day": avgPerDay,
				"distribution":     distribution,
				"high_load_days":   highLoadDays,
				"low_load_days":    lowLoadDays,
				"recommendation":   generateLoadRecommendation(distribution, avgPerDay),
			}, nil
		},
	)

	if err != nil {
		// Log error
		fmt.Printf("Failed to create weekly load analyzer function: %v\n", err)
	}

	return fn
}

func generateLoadRecommendation(distribution map[string]int, avg int) string {
	maxVariance := 0.0
	for _, count := range distribution {
		variance := float64(abs(count-avg)) / float64(avg)
		if variance > maxVariance {
			maxVariance = variance
		}
	}

	if maxVariance < 0.2 {
		return "Load is well distributed across weekdays"
	} else if maxVariance < 0.5 {
		return "Load distribution is acceptable but could be improved"
	}
	return "Consider implementing load balancing strategies for heavy days"
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
