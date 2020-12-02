package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"log"
)

type unusedResources struct {
	ConfigMap map[string]bool
	Secret    map[string]bool
}

type Options struct {
	configFlags      *genericclioptions.ConfigFlags
	namespace        string
	builder          *resource.Builder
	unusedResources  unusedResources // TODO name
	listAllNamespace bool
}

func NewPruneOptions(streams genericclioptions.IOStreams) *Options {
	return &Options{
		configFlags: genericclioptions.NewConfigFlags(true),
		unusedResources: unusedResources{
			ConfigMap: make(map[string]bool),
			Secret:    make(map[string]bool),
		},
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

	cmd.Flags().BoolVar(&o.listAllNamespace, "all-namespaces", o.listAllNamespace, "if true, prune all namespaces")

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	var err error
	o.namespace, _, err = o.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}
	o.builder = resource.NewBuilder(o.configFlags)

	result := o.builder.Unstructured().
		NamespaceParam(o.namespace).AllNamespaces(o.listAllNamespace).
		DefaultNamespace().
		ResourceTypeOrNameArgs(false, args...).
		SelectAllParam(true).
		Flatten().
		Do()

	_ = result.Visit(func(info *resource.Info, err error) error {
		switch info.Object.GetObjectKind().GroupVersionKind().Kind {
		case "ConfigMap": // FIXME do not hard code there
			o.unusedResources.ConfigMap[fmt.Sprintf("%s/%s", info.Namespace, info.Name)] = false
		case "Secret":
			secret, ok := info.Object.(metav1.Object)
			if !ok {
				return fmt.Errorf("unsupported object: %v: %s/%s", info.Mapping.Resource, info.Namespace, info.Name)
			}
			//secret. skip service-account-token, please use sa to clean
			if _, ok := secret.GetAnnotations()["kubernetes.io/service-account.name"]; ok {
				return nil
			}
			o.unusedResources.Secret[fmt.Sprintf("%s/%s", info.Namespace, info.Name)] = false
		}

		return nil
	})
	return nil
}

func (o *Options) Validate() error {
	return nil
}

func (o *Options) Run() error {
	for k, v := range o.unusedResources.Secret {
		log.Println(k, v)
	}
	return nil
}
