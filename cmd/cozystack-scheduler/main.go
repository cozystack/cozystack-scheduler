package main

import (
	"os"

	"k8s.io/component-base/cli"
	kubescheduler "k8s.io/kubernetes/cmd/kube-scheduler/app"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	fwruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"

	"github.com/cozystack/cozystack-scheduler/pkg/interpodaffinity"
	"github.com/cozystack/cozystack-scheduler/pkg/merge"
	"github.com/cozystack/cozystack-scheduler/pkg/nodeaffinity"
	"github.com/cozystack/cozystack-scheduler/pkg/podtopologyspread"
	"github.com/cozystack/cozystack-scheduler/pkg/schedulingclass"
)

var defaultLabelSelectorKeys = []string{
	merge.ApplicationGroupLabel,
	merge.ApplicationKindLabel,
	merge.ApplicationNameLabel,
}

func main() {
	fts := feature.Features{}
	cmd := kubescheduler.NewSchedulerCommand(
		kubescheduler.WithPlugin(schedulingclass.Name, schedulingclass.New),
		kubescheduler.WithPlugin(interpodaffinity.Name, fwruntime.FactoryAdapter(fts, interpodaffinity.New)),
		kubescheduler.WithPlugin(nodeaffinity.Name, fwruntime.FactoryAdapter(fts, nodeaffinity.New)),
		kubescheduler.WithPlugin(podtopologyspread.Name, fwruntime.FactoryAdapter(fts, podtopologyspread.New)),
	)

	cmd.Flags().StringSliceVar(&merge.DefaultLabelSelectorKeys, "default-label-selector-keys",
		defaultLabelSelectorKeys,
		"Pod label keys used to auto-populate empty LabelSelectors in SchedulingClass affinity and topology spread terms")

	code := cli.Run(cmd)
	os.Exit(code)
}
