module github.com/netresearch/go-cron

go 1.25

toolchain go1.25.5

retract (
	v1.2.0 // Erroneous version from inherited robfig/cron tag
	v1.3.0 // Retraction-only release; use v0.6.x
)
