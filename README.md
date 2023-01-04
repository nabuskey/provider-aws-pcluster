# provider-awspcluster

## Developing Notes
If you'd like to play with this repository while it's being developed tips to getting this to run:

This provider requires the pcluster cli available for the provider to use. There are two ways to do this:
- Global pcluster cli installed. Applicable when running in a container. 
- Virtual Environment. This is the recommended way for development purposes. 

### VEnv
```bash
python3 -m virtualenv pcluster
cd pcluster
source bin/activate
python3 -m pip install --upgrade "aws-parallelcluster"
deactivate
pwd # save this output for PYTHON_VENV_PATH env variable
```
When using VEnv, you need to set the environment variable `PYTHON_VENV_PATH` when running this provider. This instructs provider of the location where the pcluster command can be found. 
e.g.
```bash
PYTHON_VENV_PATH=/Users/manabu/provider-aws-pcluster/pcluster
```
In addition, you need to make AWS credentials available as environment variables for the provider to use:
```bash
AWS_ACCESS_KEY_ID=ACCESSKEY
AWS_SECRET_ACCESS_KEY=SECERT
AWS_SESSION_TOKEN=TOKEN
AWS_DEFAULT_REGION=us-west-2
```
Finally to run the provider locally. Note that this will connect to your current kubernetes context.
```bash
go run ./cmd/provider --debug
```


## Developing

1. Use this repository as a awspcluster to create a new one.
1. Run `make submodules` to initialize the "build" Make submodule we use for CI/CD.
1. Rename the provider by running the follwing command:
```
  make provider.prepare provider={PascalProviderName}
```
4. Add your new type by running the following command:
```
make provider.addtype provider={PascalProviderName} group={group} kind={type}
```
5. Replace the *sample* group with your new group in apis/{provider}.go
5. Replace the *mytype* type with your new type in internal/controller/{provider}.go
5. Replace the default controller and ProviderConfig implementations with your own
5. Run `make reviewable` to run code generation, linters, and tests.
5. Run `make build` to build the provider.

Refer to Crossplane's [CONTRIBUTING.md] file for more information on how the
Crossplane community prefers to work. The [Provider Development][provider-dev]
guide may also be of use.

[CONTRIBUTING.md]: https://github.com/crossplane/crossplane/blob/master/CONTRIBUTING.md
[provider-dev]: https://github.com/crossplane/crossplane/blob/master/docs/contributing/provider_development_guide.md
