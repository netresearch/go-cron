package cron

import (
	"testing"
	"time"
)

// BenchmarkParseStandard benchmarks parsing standard cron expressions.
func BenchmarkParseStandard(b *testing.B) {
	specs := []string{
		"* * * * *",
		"0 0 * * *",
		"*/5 * * * *",
		"0 9-17 * * 1-5",
		"30 4 1,15 * *",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		spec := specs[i%len(specs)]
		_, err := ParseStandard(spec)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseWithTimezone benchmarks parsing cron expressions with timezones.
func BenchmarkParseWithTimezone(b *testing.B) {
	specs := []string{
		"CRON_TZ=America/New_York 0 9 * * *",
		"CRON_TZ=Europe/London 30 8 * * 1-5",
		"TZ=Asia/Tokyo 0 0 * * *",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		spec := specs[i%len(specs)]
		_, err := ParseStandard(spec)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseDescriptor benchmarks parsing descriptor expressions.
func BenchmarkParseDescriptor(b *testing.B) {
	specs := []string{
		"@hourly",
		"@daily",
		"@weekly",
		"@monthly",
		"@yearly",
		"@every 5m",
		"@every 1h30m",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		spec := specs[i%len(specs)]
		_, err := ParseStandard(spec)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNext benchmarks calculating the next execution time.
func BenchmarkNext(b *testing.B) {
	schedule, _ := ParseStandard("*/5 * * * *")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		schedule.Next(now)
	}
}

// BenchmarkNextComplex benchmarks Next() with complex schedule.
func BenchmarkNextComplex(b *testing.B) {
	schedule, _ := ParseStandard("15,45 9-17 1,15 1-6 1-5")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		schedule.Next(now)
	}
}

// BenchmarkNextWithTimezone benchmarks Next() with timezone conversion.
func BenchmarkNextWithTimezone(b *testing.B) {
	schedule, _ := ParseStandard("CRON_TZ=America/New_York 0 9 * * *")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		schedule.Next(now)
	}
}

// BenchmarkAddJob benchmarks adding jobs to a cron instance.
func BenchmarkAddJob(b *testing.B) {
	c := New()
	job := FuncJob(func() {})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.AddJob("* * * * *", job)
	}
}

// BenchmarkCronWithManyJobs benchmarks cron operations with many jobs.
func BenchmarkCronWithManyJobs(b *testing.B) {
	for _, numJobs := range []int{10, 100, 1000} {
		b.Run(formatJobCount(numJobs), func(b *testing.B) {
			benchmarkCronWithJobs(b, numJobs)
		})
	}
}

func formatJobCount(n int) string {
	switch n {
	case 10:
		return "10_jobs"
	case 100:
		return "100_jobs"
	case 1000:
		return "1000_jobs"
	default:
		return "jobs"
	}
}

func benchmarkCronWithJobs(b *testing.B, numJobs int) {
	c := New()
	job := FuncJob(func() {})

	for i := 0; i < numJobs; i++ {
		c.AddJob("* * * * *", job)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Entries()
	}
}

// BenchmarkChainWrappers benchmarks job wrapper chain execution.
func BenchmarkChainWrappers(b *testing.B) {
	job := FuncJob(func() {})
	chain := NewChain(Recover(DiscardLogger))
	wrapped := chain.Then(job)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wrapped.Run()
	}
}
