module github.com/netresearch/go-cron

go 1.27.1

retract (
	v1.3.0 // Retraction-only release; use v0.6.x
	v1.2.0 // Erroneous version from inherited robfig/cron tag
)
