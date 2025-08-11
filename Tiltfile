# Based on https://github.com/tilt-dev/tilt-extensions/blob/master/kubebuilder/Tiltfile

load('ext://restart_process', 'docker_build_with_restart')

settings = {}

tilt_settings = "./tilt-settings.yaml" if os.path.exists("./tilt-settings.yaml") else "./tilt-settings.json"
settings.update(read_yaml(tilt_settings, default = {}))

if 'allowed_contexts' in settings:
    allow_k8s_contexts(settings['allowed_contexts'])
if 'default_registry' in settings:
    # Set the default registry for tilt to use
    default_registry(settings['default_registry'].removesuffix('/'))
if 'feature_gates' in settings:
    # Set the feature gates for the controller
    feature_gates = settings['feature_gates']
else:
    feature_gates = {
        'vLLM': True,
        'gatewayAPIInferenceExtension': True,
    }

def main(IMG='controller:latest', DISABLE_SECURITY_CONTEXT=True):

    DOCKERFILE = '''
    FROM --platform=linux/amd64 mcr.microsoft.com/oss/go/microsoft/golang:1.24
    WORKDIR /
    COPY ./tilt_bin/manager /
    COPY ./presets/workspace/models/supported_models.yaml /
    CMD ["/manager"]
    '''

    def yaml():
        cluster_name = k8s_context() if not settings.get('cluster_name') else settings['cluster_name']
        helm_template = 'helm template kaito-workspace ./charts/kaito/workspace --namespace kaito-workspace --set clusterName={} --set image.repository=controller --set image.tag=latest --set featureGates.gatewayAPIInferenceExtension={}'.format(cluster_name, feature_gates['gatewayAPIInferenceExtension'])
        # Set the image name and tag to controller:latest for Tilt to
        # substitute later during docker_build_with_restart
        data = local(helm_template, quiet=True)
        # Tilt's live update conflicts with SecurityContext, until a better solution, just remove it
        if DISABLE_SECURITY_CONTEXT:
            decoded = decode_yaml_stream(data)
            if decoded:
                for d in decoded:
                    # Workaround: Force the namespace to "kaito-workspace" because `helm template` does not correctly render
                    # the subchart namespace. See: https://github.com/fluxcd-community/helm-charts/issues/239.
                    if "namespace" not in d["metadata"]:
                        d["metadata"]["namespace"] = "kaito-workspace"
                    if d["kind"] == "Deployment":
                        if "securityContext" in d['spec']['template']['spec']:
                            d['spec']['template']['spec'].pop('securityContext')
                        for c in d['spec']['template']['spec']['containers']:
                            if "securityContext" in c:
                                c.pop('securityContext')
            return encode_yaml_stream(decoded)
        return data

    def make_manifests():
        return 'make manifests'

    def make_generate():
        return 'make generate'

    def create_namespace(name):
        return 'kubectl create namespace {} || true'.format(name)

    def manager():
        return 'CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o tilt_bin/manager cmd/workspace/*.go'

    # Create the namespace if it doesn't exist
    local(create_namespace('kaito-workspace'), quiet=True)

    # Generate the CRD manifests and kubectl apply them. Re-generate if anything in deps changes.
    local_resource(
        'crd',
        make_generate() + ' && ' + make_manifests() + ' && kubectl apply --server-side -f charts/kaito/workspace/crds',
        deps=['api'],
        ignore=['*/*/zz_generated.deepcopy.go'],
        labels='Workspace',
    )

    # Deploy the rendered helm chart
    k8s_yaml(yaml())

    # Labels resources from kaito-workspace
    k8s_resource(
        workload='kaito-workspace',
        new_name='deployment',
        labels='Workspace',
        objects=['kaito-workspace-sa:serviceaccount',
                 'kaito-workspace-role:role',
                 'kaito-workspace-clusterrole:clusterrole',
                 'kaito-workspace-rolebinding:rolebinding',
                 'kaito-workspace-rolebinding:clusterrolebinding',
                 'inference-params-template:configmap',
                 'lora-params-template:configmap',
                 'qlora-params-template:configmap',
                 'workspace-webhook-cert:secret',
                 'validation.workspace.kaito.sh:validatingwebhookconfiguration',
        ],
    )
    k8s_resource(
        workload='nvidia-device-plugin-daemonset',
        new_name='device plugin',
        labels='Dependencies',
    )

    # Re-compile the manager binary when anything in deps changes.
    local_resource(
        'manager',
        manager(),
        deps=['api', 'cmd', 'pkg', 'presets'],
        ignore=['*/*/zz_generated.deepcopy.go'],
        labels='Workspace',
    )

    # Build the initial controller image. When a newer manager binary is built,
    # Tilt will replace the old one in the running container with the new one.
    docker_build_with_restart(IMG, '.',
     dockerfile_contents=DOCKERFILE,
     entrypoint='/manager --feature-gates={}'.format(
        ','.join(['{}={}'.format(k, str(v).lower()) for k, v in feature_gates.items()])
     ),
     only=['./tilt_bin/manager', './presets/workspace/models/supported_models.yaml'],
     live_update=[
           sync('./tilt_bin/manager', '/manager'),
           sync('./presets/workspace/models/supported_models.yaml', '/supported_models.yaml'),
       ]
    )

main()
