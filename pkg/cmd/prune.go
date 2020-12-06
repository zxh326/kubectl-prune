package cmd

import (
	"fmt"
	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/klog/v2"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"log"
	"strings"
)

type needPruneResource struct {
	Namespace  string
	Kind       string
	ObjectName string
}

type Options struct {
	configFlags *genericclioptions.ConfigFlags

	usedConfigMaps      map[string]bool
	usedSecrets         map[string]bool
	usedServiceAccounts map[string]bool
	ignoreNameSpaces    string

	GracePeriod   int
	ForceDeletion bool

	namespace     string
	LabelSelector string
	FieldSelector string
	AllNamespaces bool
	Yes           bool

	DryRunStrategy cmdutil.DryRunStrategy
	DryRunVerifier *resource.DryRunVerifier
	Quiet          bool
}

func NewPruneOptions(streams genericclioptions.IOStreams) *Options {
	return &Options{
		configFlags:         genericclioptions.NewConfigFlags(true),
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
			f := cmdutil.NewFactory(o.configFlags)

			if err := o.Complete(f, c, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Run(args); err != nil {
				return err
			}
			return nil
		},
	}

	cmdutil.AddDryRunFlag(cmd)

	cmd.Flags().StringVarP(&o.LabelSelector, "selector", "l", o.LabelSelector, "Selector (label query) to filter on, supports '=', '==', and '!='.(e.g. -l key1=value1,key2=value2)")
	cmd.Flags().StringVar(&o.FieldSelector, "field-selector", o.FieldSelector, "Selector (field query) to filter on, supports '=', '==', and '!='.(e.g. --field-selector key1=value1,key2=value2). The server only supports a limited number of field queries per type.")
	cmd.Flags().StringVar(&o.ignoreNameSpaces, "ignore-namespaces", o.ignoreNameSpaces, "If present, will ignore these namespace resources")
	cmd.Flags().BoolVarP(&o.AllNamespaces, "all-namespaces", "A", o.AllNamespaces, "If present, list the requested object(s) across all namespaces. Namespace in current context is ignored even if specified with --namespace.")
	cmd.Flags().BoolVarP(&o.Yes, "yes", "y", o.Yes, "Automatic yes to prompts; assume \"yes\" as answer to all prompts and run non-interactively.")
	cmd.Flags().IntVar(&o.GracePeriod, "grace-period", -1, "Period of time in seconds given to the resource to terminate gracefully. Ignored if negative. Set to 1 for immediate shutdown. Can only be set to 0 when --force is true (force deletion).")
	cmd.Flags().BoolVar(&o.ForceDeletion, "force", false, "If true, immediately remove resources from API and bypass graceful deletion. Note that immediate deletion of some resources may result in inconsistency or data loss and requires confirmation.")
	cmd.Flags().BoolVarP(&o.Quiet, "quiet", "q", false, "If true, no output is produced")

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func (o *Options) Complete(f cmdutil.Factory, c *cobra.Command, args []string) error {
	var err error
	// copy from https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/kubectl/pkg/cmd/delete/delete.go
	if o.GracePeriod == 0 && !o.ForceDeletion {
		// To preserve backwards compatibility, but prevent accidental data loss, we convert --grace-period=0
		// into --grace-period=1. Users may provide --force to bypass this conversion.
		o.GracePeriod = 1
	}
	if o.ForceDeletion && o.GracePeriod < 0 {
		o.GracePeriod = 0
	}

	o.DryRunStrategy, err = cmdutil.GetDryRunStrategy(c)
	if err != nil {
		return err
	}
	dynamicClient, err := f.DynamicClient()
	if err != nil {
		return err
	}
	discoveryClient, err := f.ToDiscoveryClient()
	if err != nil {
		return err
	}
	o.DryRunVerifier = resource.NewDryRunVerifier(dynamicClient, discoveryClient)

	o.namespace, _, err = o.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	podList, _ := o.getPods()
	for _, pod := range podList.Items {
		o.checkUsedResources(&pod)
	}
	return nil
}

func (o *Options) Validate() error {
	return nil
}

func (o *Options) Run(args []string) error {
	builder := resource.NewBuilder(o.configFlags)
	result := builder.Unstructured().
		NamespaceParam(o.namespace).DefaultNamespace().AllNamespaces(o.AllNamespaces).
		ResourceTypeOrNameArgs(false, args...).
		LabelSelectorParam(o.LabelSelector).
		FieldSelectorParam(o.FieldSelector).
		SelectAllParam(true).
		Flatten().
		Do()

	err := result.Visit(func(info *resource.Info, err error) error {
		if info.Namespace == "kube-system" {
			return nil
		}
		if strings.Contains(o.ignoreNameSpaces, info.Namespace) {
			return nil
		}
		res := &needPruneResource{
			Namespace:  info.Namespace,
			Kind:       info.Object.GetObjectKind().GroupVersionKind().Kind,
			ObjectName: info.Name,
		}

		switch res.Kind {
		case "ConfigMap": // FIXME do not hard code here
			if o.usedConfigMaps[info.Namespace+"/"+info.Name] {
				return nil
			}
		case "Secret":
			unstr, ok := info.Object.(*unstructured.Unstructured)
			if !ok {
				return fmt.Errorf("attempt to decode non-Unstructured object")
			}

			secret := &v1.Secret{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstr.Object, secret); err != nil {
				return fmt.Errorf("unsupported object: %v: %s/%s", info.Mapping.Resource, info.Namespace, info.Name)
			}

			// skip
			if secret.Type == "kubernetes.io/service-account-token" ||
				secret.Type == "kubernetes.io/dockercfg" ||
				secret.Type == "kubernetes.io/dockerconfigjson" {
				return nil
			}

			if o.usedServiceAccounts[info.Namespace+"/"+info.Name] {
				return nil
			}
		default:
			klog.Infof("unsupported prune object: %v: %s", info.Mapping.Resource, info.ObjectName())
		}

		// need cleanup
		confirmDelete := o.Yes
		if !confirmDelete && o.DryRunStrategy == 0 {
			prompt := &survey.Confirm{
				Message: fmt.Sprintf("Delete %s %s/%s?", res.Namespace, res.Kind, res.ObjectName),
			}
			err = survey.AskOne(prompt, &confirmDelete)
			if err != nil {
				return err
			}
		}

		if confirmDelete || o.DryRunStrategy != 0 {
			options := &metav1.DeleteOptions{}
			if o.GracePeriod >= 0 {
				options = metav1.NewDeleteOptions(int64(o.GracePeriod))
			}

			if o.DryRunStrategy == cmdutil.DryRunClient {
				if !o.Quiet {
					o.PrintObj(info)
				}
				return nil
			}
			if o.DryRunStrategy == cmdutil.DryRunServer {
				if err := o.DryRunVerifier.HasSupport(info.Mapping.GroupVersionKind); err != nil {
					return err
				}
			}

			// TODO backup, wait
			_, err := o.deleteResource(info, options)

			if err != nil {
				return err
			}
		}
		return nil
	})

	return err
}

func (o *Options) getPods() (*v1.PodList, error) {
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

	err := result.Visit(func(info *resource.Info, err error) error {
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

	return podList, err
}

func (o *Options) checkUsedResources(pod *v1.Pod) {
	for _, c := range pod.Spec.Containers {
		for _, envFrom := range c.EnvFrom {
			if envFrom.ConfigMapRef != nil {
				o.usedConfigMaps[pod.Namespace+"/"+envFrom.ConfigMapRef.Name] = true
			}
			if envFrom.SecretRef != nil {
				o.usedSecrets[pod.Namespace+"/"+envFrom.SecretRef.Name] = true
			}
		}

		for _, env := range c.Env {
			if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
				o.usedConfigMaps[pod.Namespace+"/"+env.ValueFrom.ConfigMapKeyRef.Name] = true
			}
		}
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.ConfigMap != nil {
			o.usedConfigMaps[pod.Namespace+"/"+volume.ConfigMap.Name] = true
		}
		if volume.Secret != nil {
			o.usedSecrets[pod.Namespace+"/"+volume.Secret.SecretName] = true
		}
	}
	o.usedServiceAccounts[pod.Namespace+"/"+pod.Spec.ServiceAccountName] = true
}

// copy from https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/kubectl/pkg/cmd/delete/delete.go#L392
func (o *Options) deleteResource(info *resource.Info, options *metav1.DeleteOptions) (runtime.Object, error) {
	resp, err := resource.
		NewHelper(info.Client, info.Mapping).
		DryRun(o.DryRunStrategy == cmdutil.DryRunServer).
		DeleteWithOptions(info.Namespace, info.Name, options)

	if err != nil {
		return nil, cmdutil.AddSourceToErr("deleting", info.Source, err)
	}

	if !o.Quiet {
		o.PrintObj(info)
	}
	return resp, err

}

func (o *Options) PrintObj(info *resource.Info) {
	operation := "deleted"
	groupKind := info.Mapping.GroupVersionKind
	kindString := fmt.Sprintf("%s.%s", strings.ToLower(groupKind.Kind), groupKind.Group)
	if len(groupKind.Group) == 0 {
		kindString = strings.ToLower(groupKind.Kind)
	}

	if o.GracePeriod == 0 {
		operation = "force deleted"
	}

	switch o.DryRunStrategy {
	case cmdutil.DryRunClient:
		operation = fmt.Sprintf("%s (dry run)", operation)
	case cmdutil.DryRunServer:
		operation = fmt.Sprintf("%s (server dry run)", operation)
	}

	// understandable output by default
	log.Printf("%s \"%s\" %s\n", kindString, info.Name, operation)
}
