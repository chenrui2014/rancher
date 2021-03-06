package pipelineexecution

import (
	"context"
	"fmt"
	"github.com/rancher/rancher/pkg/controllers/user/pipeline/engine"
	"github.com/rancher/rancher/pkg/controllers/user/pipeline/utils"
	"github.com/rancher/rancher/pkg/ticker"
	"github.com/rancher/types/apis/management.cattle.io/v3"
	"github.com/sirupsen/logrus"
	"strings"
	"time"
)

const (
	syncLogInterval = 10 * time.Second
)

//ExecutionLogSyncer is responsible for updating pipeline execution logs that are in building state
//by syncing with the pipeline engine
type ExecutionLogSyncer struct {
	clusterName           string
	clusterPipelineLister v3.ClusterPipelineLister

	pipelineExecutionLister    v3.PipelineExecutionLister
	pipelineExecutionLogLister v3.PipelineExecutionLogLister
	pipelineExecutionLogs      v3.PipelineExecutionLogInterface
	pipelineEngine             engine.PipelineEngine
}

func (s *ExecutionLogSyncer) sync(ctx context.Context, syncInterval time.Duration) {
	for range ticker.Context(ctx, syncInterval) {
		s.syncLogs()
	}

}

func (s *ExecutionLogSyncer) syncLogs() {
	if !utils.IsPipelineDeploy(s.clusterPipelineLister, s.clusterName) {
		return
	}

	Logs, err := s.pipelineExecutionLogLister.List("", utils.PipelineInprogressLabel.AsSelector())
	if err != nil {
		logrus.Errorf("Error listing PipelineExecutionLogs - %v", err)
		return
	}
	if len(Logs) < 1 {
		return
	}
	if err := s.pipelineEngine.PreCheck(); err != nil {
		logrus.Errorf("Error get Jenkins engine - %v", err)
		return
	}
	for _, e := range Logs {
		parts := strings.Split(e.Spec.PipelineExecutionName, ":")
		if len(parts) != 2 {
			e.Spec.Message += fmt.Sprintf("\nInvalid pipeline execution name - %s", e.Spec.PipelineExecutionName)
			e.Labels["pipeline.management.cattle.io/finish"] = "true"
			if _, err := s.pipelineExecutionLogs.Update(e); err != nil {
				logrus.Errorf("Error update pipeline execution log - %v", err)
				return
			}
			continue
		}
		execution, err := s.pipelineExecutionLister.Get(e.Namespace, parts[1])
		if err != nil {
			logrus.Errorf("Error get pipeline execution - %v", err)
			e.Spec.Message += fmt.Sprintf("\nError get pipeline execution - %v", err)
			e.Labels["pipeline.management.cattle.io/finish"] = "true"
			if _, err := s.pipelineExecutionLogs.Update(e); err != nil {
				logrus.Errorf("Error update pipeline execution log - %v", err)
				return
			}
			continue
		}
		//get log if the step started
		if execution.Status.Stages[e.Spec.Stage].Steps[e.Spec.Step].State == utils.StateWaiting {
			continue
		}
		logText, err := s.pipelineEngine.GetStepLog(execution, e.Spec.Stage, e.Spec.Step)
		if err != nil {
			logrus.Errorf("Error get pipeline execution log - %v", err)
			e.Spec.Message += fmt.Sprintf("\nError get pipeline execution log - %v", err)
			e.Labels["pipeline.management.cattle.io/finish"] = "true"
			if _, err := s.pipelineExecutionLogs.Update(e); err != nil {
				logrus.Errorf("Error update pipeline execution log - %v", err)
			}
			continue
		}

		e.Spec.Message = logText
		stepState := execution.Status.Stages[e.Spec.Stage].Steps[e.Spec.Step].State
		if stepState != utils.StateWaiting && stepState != utils.StateBuilding {
			e.Labels["pipeline.management.cattle.io/finish"] = "true"
		}
		if _, err := s.pipelineExecutionLogs.Update(e); err != nil {
			logrus.Errorf("Error update pipeline execution log - %v", err)
			return
		}
	}
	logrus.Debugf("Sync pipeline execution log complete")
}
