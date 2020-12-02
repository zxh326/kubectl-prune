package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/klog/v2"
	"log"
)

type needPruneResource struct {
	Namespace  string
	Kind       string
	ObjectName string
	Used       bool
}

type Options struct {
	configFlags *genericclioptions.ConfigFlags

	needPruneResources []*needPruneResource

	usedConfigMaps      map[string]bool
	usedSecrets         map[string]bool
	usedServiceAccounts map[string]bool

	namespace     string
	LabelSelector string
	FieldSelector string
	AllNamespaces bool
}

func NewPruneOptions(streams genericclioptions.IOStreams) *Options {
	return &Options{
		configFlags:         genericclioptions.NewConfigFlags(true),
		needPruneResources:  make([]*needPruneResource, 0, 100),
		usedConfigMaps:      map[string]bool{},
		usedSecrets:         map[string]bool{},
		usedServiceAccounts: map[string]bool{},
	}
}

func NewCmdPrune(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewPruneOptions(streams)

	cmd := &cobra.Command{
		Use:          "prune [resources] [flags]",
		Short:        "Remove unused resources",
		Example:      "kubectl prune configmap",
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&o.LabelSelector, "selector", "l", o.LabelSelector, "Selector (label query) to filter on, supports '=', '==', and '!='.(e.g. -l key1=value1,key2=value2)")
	cmd.Flags().StringVar(&o.FieldSelector, "field-selector", o.FieldSelector, "Selector (field query) to filter on, supports '=', '==', and '!='.(e.g. --field-selector key1=value1,key2=value2). The server only supports a limited number of field queries per type.")
	cmd.Flags().BoolVarP(&o.AllNamespaces, "all-namespaces", "A", o.AllNamespaces, "If present, list the requested object(s) across all namespaces. Namespace in current context is ignored even if specified with --namespace.")

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	var err error
	o.namespace, _, err = o.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}
	podList := o.getPods()

	for _, pod := range podList.Items {
		for _, c := range pod.Spec.Containers {
			log.Println(c.Name)
		}
	}

	builder := resource.NewBuilder(o.configFlags)

	result := builder.Unstructured().
		NamespaceParam(o.namespace).DefaultNamespace().AllNamespaces(o.AllNamespaces).
		ResourceTypeOrNameArgs(false, args...).
		LabelSelectorParam(o.LabelSelector).
		FieldSelectorParam(o.FieldSelector).
		SelectAllParam(true).
		Flatten().
		Do()

	_ = result.Visit(func(info *resource.Info, err error) error {
		switch kind := info.Object.GetObjectKind().GroupVersionKind().Kind; kind {
		case "ConfigMap": // FIXME do not hard code here
			o.needPruneResources = append(o.needPruneResources, &needPruneResource{
				Namespace:  info.Namespace,
				Kind:       kind,
				ObjectName: info.Name,
			})
		case "Secret":
			unstr, ok := info.Object.(*unstructured.Unstructured)
			if !ok {
				return fmt.Errorf("attempt to decode non-Unstructured object")
			}

			secret := &v1.Secret{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstr.Object, secret); err != nil {
				return fmt.Errorf("unsupported object: %v: %s/%s", info.Mapping.Resource, info.Namespace, info.Name)
			}

			// skip service-account-token, please use sa to clean
			if secret.Type == "kubernetes.io/service-account-token" {
				return nil
			}

			o.needPruneResources = append(o.needPruneResources, &needPruneResource{
				Namespace:  info.Namespace,
				Kind:       kind,
				ObjectName: info.Name,
			})
		default:
			klog.Infof("unsupported prune object: %v: %s", info.Mapping.Resource, info.ObjectName())
		}

		return nil
	})
	return nil
}

func (o *Options) Validate() error {
	return nil
}

func (o *Options) Run() error {
	for _, v := range o.needPruneResources {
		log.Printf("%+v", v)
	}
	return nil
}

func (o *Options) getPods() *v1.PodList {
	result := resource.NewBuilder(o.configFlags).Unstructured().
		NamespaceParam(o.namespace).DefaultNamespace().AllNamespaces(o.AllNamespaces).
		ResourceTypeOrNameArgs(false, "pod").
		LabelSelectorParam(o.LabelSelector).
		FieldSelectorParam(o.FieldSelector).
		SelectAllParam(true).
		Latest().
		Flatten().
		Do()

	podList := &v1.PodList{Items: []v1.Pod{}}

	_ = result.Visit(func(info *resource.Info, err error) error {
		unstr, ok := info.Object.(*unstructured.Unstructured)
		pod := v1.Pod{}
		if !ok {
			return fmt.Errorf("attempt to decode non-Unstructured object")
		}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstr.Object, &pod); err != nil {
			return fmt.Errorf("unsupported object: %v: %s/%s", info.Mapping.Resource, info.Namespace, info.Name)
		}
		podList.Items = append(podList.Items, pod)
		return nil
	})

	return podList
}
