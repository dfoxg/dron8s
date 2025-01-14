package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

func main() {
	// Lookup for env variable `PLUGIN_KUBECONFIG`.
	kubeconfig, exists := os.LookupEnv("PLUGIN_KUBECONFIG")
	switch exists {
	// If it does exists means user intents for out-of-cluster usage with provided kubeconfig
	case true:
		data := []byte(kubeconfig)
		// create a kubeconfig file
		err := os.WriteFile("./kubeconfig", data, 0644)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		outOfCluster, err := clientcmd.BuildConfigFromFlags("", "./kubeconfig")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		fmt.Println("Out-of-cluster SSA initiliazing")
		err = ssa(context.Background(), outOfCluster)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

	// If user didn't provide a kubeconfig dron8s defaults to create an in-cluster config
	case false:
		inCluster, err := rest.InClusterConfig()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		fmt.Println("In-cluster SSA initiliazing")
		err = ssa(context.Background(), inCluster)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}

}

// https://ymmt2005.hatenablog.com/entry/2020/04/14/An_example_of_using_dynamic_client_of_k8s.io/client-go#Go-client-libraries
func ssa(ctx context.Context, cfg *rest.Config) error {
	// 1. Prepare a RESTMapper to find GVR
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	// 2. Prepare the dynamic client
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}

	// 2.1. Read user's yaml
	yaml, err := os.ReadFile(os.Getenv("PLUGIN_YAML"))
	if err != nil {
		return err
	}

	configs, err := parseYamlAndSplit(yaml)

	if err != nil {
		return err
	}

	// Iterate over provided configs
	var sum int
	sum, err = applyYAML(configs, mapper, dyn, ctx)

	if nil != err {
		fmt.Println("Failed to apply changes")
		fmt.Println(err)
		return err
	}

	fmt.Println("Dron8s finished applying ", sum, " configs.")

	return nil
}

func parseYamlAndSplit(yaml []byte) ([]string, error) {
	// convert it to string
	text := string(yaml)
	// Parse variables
	t := template.Must(template.New("dron8s").Option("missingkey=error").Parse(text))
	b := bytes.NewBuffer(make([]byte, 0, 512))
	err := t.Execute(b, getVariablesFromDrone())
	if err != nil {
		return nil, err
	}
	text = b.String()
	// Parse each yaml from file
	configs := strings.Split(text, "\n---\n")
	return configs, nil
}

func applyYAML(configs []string, mapper *restmapper.DeferredDiscoveryRESTMapper, dyn *dynamic.DynamicClient, ctx context.Context) (int, error) {
	var decUnstructured = yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

	// variable to hold and print how many yaml configs are present
	var sum int
	for i, v := range configs {

		// If a yaml starts with `---`
		// the first slice of `configs` will be empty
		// so we just skip (continue) to next iteration
		if len(v) == 0 {
			continue
		}

		// 3. Decode YAML manifest into unstructured.Unstructured
		obj := &unstructured.Unstructured{}
		_, gvk, err := decUnstructured.Decode([]byte(v), nil, obj)
		if err != nil {
			return sum, err
		}

		if gvk.String() == "kustomize.config.k8s.io/v1beta1, Kind=Kustomization" {
			fmt.Printf("detected kustomize file on index %d\n", i)
			k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())

			filepath := filepath.Dir(os.Getenv("PLUGIN_YAML"))

			fmt.Printf("run kustomize on %s\n", filepath)
			m, err := k.Run(filesys.MakeFsOnDisk(), filepath)
			if nil != err {
				fmt.Println("failed to apply kustomize")
				fmt.Println(err)
				return sum, err
			}
			raw, _ := m.AsYaml()
			yamlString := strings.Split(string(raw), "\n---\n")
			fmt.Printf("appying %d resources from kustomize\n", len(yamlString))
			fmt.Println("=======================================================")
			intSum, err := applyYAML(yamlString, mapper, dyn, ctx)
			if nil != err {
				fmt.Println("failed to apply following kustomize yaml")
				fmt.Println(yamlString)
				fmt.Println(err)
				return sum, err
			}
			fmt.Println("=======================================================")
			sum += intSum
			continue
		}

		// 4. Find GVR
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return sum, err
		}

		// 5. Obtain REST interface for the GVR
		var dr dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			if obj.GetNamespace() == "" {
				obj.SetNamespace("default")
			}
			dr = dyn.Resource(mapping.Resource).Namespace(obj.GetNamespace())
		} else {
			// for cluster-wide resources
			dr = dyn.Resource(mapping.Resource)
		}

		// 6. Marshal object into JSON
		data, err := json.Marshal(obj)
		if err != nil {
			return sum, err
		}

		fmt.Println("Applying config #", i)
		// 7. Create or Update the object with SSA
		//     types.ApplyPatchType indicates SSA.
		//     FieldManager specifies the field owner ID.
		_, err = dr.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
			FieldManager: "dron8s-plugin",
		})
		if err != nil {
			return sum, err
		}

		sum++
	}
	return sum, nil
}

func getVariablesFromDrone() map[string]string {
	ctx := make(map[string]string)
	pluginEnv := os.Environ()
	pluginReg := regexp.MustCompile(`^PLUGIN_(.*)=(.*)`)
	droneReg := regexp.MustCompile(`^DRONE_(.*)=(.*)`)

	for _, value := range pluginEnv {
		if pluginReg.MatchString(value) {
			matches := pluginReg.FindStringSubmatch(value)
			key := strings.ToLower(matches[1])
			ctx[key] = matches[2]
		}

		if droneReg.MatchString(value) {
			matches := droneReg.FindStringSubmatch(value)
			key := strings.ToLower(matches[1])
			ctx[key] = matches[2]
		}
	}
	return ctx
}
