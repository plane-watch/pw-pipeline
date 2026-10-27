package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const metricsNamespace = "pw_archiver"

var (
	metBatches = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "batches_total",
		Help:      "Raw archive batches received and filed.",
	})
	metBatchesBad = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "batches_bad_total",
		Help:      "Messages on the subject that were not parseable batches.",
	})
	metBatchesLate = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "batches_late_total",
		Help:      "Batches for a minute already sealed, or before the archiver started; their records were dropped.",
	})
	metBatchesDropped = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "batches_with_drops_total",
		Help:      "Batches whose instance reported frames dropped from its queue.",
	})
	metRecords = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "records_total",
		Help:      "Records filed into a partition, by batch record type.",
	}, []string{"type"})
	metRecordsDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "records_dropped_total",
		Help:      "Records not filed, by reason.",
	}, []string{"reason"})
	metDuplicates = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "duplicates_total",
		Help:      "Copies of a message merged into an earlier record.",
	})
	metPartitions = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "partitions_total",
		Help:      "Partitions sealed, by outcome: written, exists (another archiver wrote it first), failed.",
	}, []string{"result"})
	metPartitionsIncomplete = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "partitions_incomplete_total",
		Help:      "Partitions sealed with the incomplete flag.",
	})
	metPartitionBytes = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "partition_bytes_total",
		Help:      "Bytes of partition objects written.",
	})
	metInstances = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "instances",
		Help:      "Ingest instances heard from recently.",
	})
	metOpenMinutes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "open_minutes",
		Help:      "Minutes being filled, not yet sealed.",
	})
	metSubscriptionDropped = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "subscription_dropped_total",
		Help:      "Batches the NATS client discarded because this archiver fell behind (slow consumer).",
	})
	metDedupeEntries = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "dedupe_entries",
		Help:      "Messages held in the de-duplication window.",
	})
	prometheusAppVer = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "info",
		Help:      "Application Info/Metadata",
	}, []string{"version"})
)

const (
	dropUnknownTile  = "unknown_tile"
	dropMLATMarker   = "mlat_marker"
	dropNoPosition   = "no_position"
	dropTooLong      = "too_long"
	dropBadLength    = "bad_length"
	dropFilteredTile = "filtered_tile"
	dropLate         = "late"
	dropUnknownType  = "unknown_type"
)
