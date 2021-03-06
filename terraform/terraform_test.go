package terraform

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/go-getter"
	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/config/module"
	"github.com/hashicorp/terraform/helper/logging"
)

// This is the directory where our test fixtures are.
const fixtureDir = "./test-fixtures"

func TestMain(m *testing.M) {
	// Experimental features
	xNewApply := flag.Bool("Xnew-apply", false, "Experiment: new apply graph")

	flag.Parse()

	// Setup experimental features
	X_newApply = *xNewApply
	if X_newApply {
		println("Xnew-apply enabled")
	}

	if testing.Verbose() {
		// if we're verbose, use the logging requested by TF_LOG
		logging.SetOutput()
	} else {
		// otherwise silence all logs
		log.SetOutput(ioutil.Discard)
	}

	// Make sure shadow operations fail our real tests
	contextFailOnShadowError = true

	// Always DeepCopy the Diff on every Plan during a test
	contextTestDeepCopyOnPlan = true

	// Shadow the new graphs
	contextTestShadow = true

	os.Exit(m.Run())
}

func tempDir(t *testing.T) string {
	dir, err := ioutil.TempDir("", "tf")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("err: %s", err)
	}

	return dir
}

// tempEnv lets you temporarily set an environment variable. It returns
// a function to defer to reset the old value.
// the old value that should be set via a defer.
func tempEnv(t *testing.T, k string, v string) func() {
	old := os.Getenv(k)
	os.Setenv(k, v)
	return func() { os.Setenv(k, old) }
}

func testConfig(t *testing.T, name string) *config.Config {
	c, err := config.LoadFile(filepath.Join(fixtureDir, name, "main.tf"))
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	return c
}

func testModule(t *testing.T, name string) *module.Tree {
	mod, err := module.NewTreeModule("", filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	s := &getter.FolderStorage{StorageDir: tempDir(t)}
	if err := mod.Load(s, module.GetModeGet); err != nil {
		t.Fatalf("err: %s", err)
	}

	return mod
}

// testModuleInline takes a map of path -> config strings and yields a config
// structure with those files loaded from disk
func testModuleInline(t *testing.T, config map[string]string) *module.Tree {
	cfgPath, err := ioutil.TempDir("", "tf-test")
	if err != nil {
		t.Errorf("Error creating temporary directory for config: %s", err)
	}
	defer os.RemoveAll(cfgPath)

	for path, configStr := range config {
		dir := filepath.Dir(path)
		if dir != "." {
			err := os.MkdirAll(filepath.Join(cfgPath, dir), os.FileMode(0777))
			if err != nil {
				t.Fatalf("Error creating subdir: %s", err)
			}
		}
		// Write the configuration
		cfgF, err := os.Create(filepath.Join(cfgPath, path))
		if err != nil {
			t.Fatalf("Error creating temporary file for config: %s", err)
		}

		_, err = io.Copy(cfgF, strings.NewReader(configStr))
		cfgF.Close()
		if err != nil {
			t.Fatalf("Error creating temporary file for config: %s", err)
		}
	}

	// Parse the configuration
	mod, err := module.NewTreeModule("", cfgPath)
	if err != nil {
		t.Fatalf("Error loading configuration: %s", err)
	}

	// Load the modules
	modStorage := &getter.FolderStorage{
		StorageDir: filepath.Join(cfgPath, ".tfmodules"),
	}
	err = mod.Load(modStorage, module.GetModeGet)
	if err != nil {
		t.Errorf("Error downloading modules: %s", err)
	}

	return mod
}

func testStringMatch(t *testing.T, s fmt.Stringer, expected string) {
	actual := strings.TrimSpace(s.String())
	expected = strings.TrimSpace(expected)
	if actual != expected {
		t.Fatalf("Actual\n\n%s\n\nExpected:\n\n%s", actual, expected)
	}
}

func testProviderFuncFixed(rp ResourceProvider) ResourceProviderFactory {
	return func() (ResourceProvider, error) {
		return rp, nil
	}
}

func testProvisionerFuncFixed(rp ResourceProvisioner) ResourceProvisionerFactory {
	return func() (ResourceProvisioner, error) {
		return rp, nil
	}
}

// HookRecordApplyOrder is a test hook that records the order of applies
// by recording the PreApply event.
type HookRecordApplyOrder struct {
	NilHook

	Active bool

	IDs    []string
	States []*InstanceState
	Diffs  []*InstanceDiff

	l sync.Mutex
}

func (h *HookRecordApplyOrder) PreApply(
	info *InstanceInfo,
	s *InstanceState,
	d *InstanceDiff) (HookAction, error) {
	if h.Active {
		h.l.Lock()
		defer h.l.Unlock()

		h.IDs = append(h.IDs, info.Id)
		h.Diffs = append(h.Diffs, d)
		h.States = append(h.States, s)
	}

	return HookActionContinue, nil
}

// Below are all the constant strings that are the expected output for
// various tests.

const testTerraformInputProviderStr = `
aws_instance.bar:
  ID = foo
  bar = override
  foo = us-east-1
  type = aws_instance
aws_instance.foo:
  ID = foo
  bar = baz
  num = 2
  type = aws_instance
`

const testTerraformInputProviderOnlyStr = `
aws_instance.foo:
  ID = foo
  foo = us-west-2
  type = aws_instance
`

const testTerraformInputVarOnlyStr = `
aws_instance.foo:
  ID = foo
  foo = us-east-1
  type = aws_instance
`

const testTerraformInputVarOnlyUnsetStr = `
aws_instance.foo:
  ID = foo
  bar = baz
  foo = foovalue
  type = aws_instance
`

const testTerraformInputVarsStr = `
aws_instance.bar:
  ID = foo
  bar = override
  foo = us-east-1
  type = aws_instance
aws_instance.foo:
  ID = foo
  bar = baz
  num = 2
  type = aws_instance
`

const testTerraformApplyStr = `
aws_instance.bar:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.foo:
  ID = foo
  num = 2
  type = aws_instance
`

const testTerraformApplyRefCountStr = `
aws_instance.bar:
  ID = foo
  foo = 3
  type = aws_instance

  Dependencies:
    aws_instance.foo
aws_instance.foo.0:
  ID = foo
aws_instance.foo.1:
  ID = foo
aws_instance.foo.2:
  ID = foo
`

const testTerraformApplyProviderAliasStr = `
aws_instance.bar:
  ID = foo
  provider = aws.bar
  foo = bar
  type = aws_instance
aws_instance.foo:
  ID = foo
  num = 2
  type = aws_instance
`

const testTerraformApplyEmptyModuleStr = `
<no state>
Outputs:

end = XXXX

module.child:
<no state>
Outputs:

aws_access_key = YYYYY
aws_route53_zone_id = XXXX
aws_secret_key = ZZZZ
`

const testTerraformApplyDependsCreateBeforeStr = `
aws_instance.lb:
  ID = foo
  instance = foo
  type = aws_instance

  Dependencies:
    aws_instance.web
aws_instance.web:
  ID = foo
  require_new = ami-new
  type = aws_instance
`

const testTerraformApplyCreateBeforeStr = `
aws_instance.bar:
  ID = foo
  require_new = xyz
  type = aws_instance
`

const testTerraformApplyCreateBeforeUpdateStr = `
aws_instance.bar:
  ID = foo
  foo = baz
  type = aws_instance
`

const testTerraformApplyCancelStr = `
aws_instance.foo:
  ID = foo
  num = 2
`

const testTerraformApplyComputeStr = `
aws_instance.bar:
  ID = foo
  foo = computed_dynamical
  type = aws_instance

  Dependencies:
    aws_instance.foo
aws_instance.foo:
  ID = foo
  dynamical = computed_dynamical
  num = 2
  type = aws_instance
`

const testTerraformApplyCountDecStr = `
aws_instance.foo.0:
  ID = bar
  foo = foo
  type = aws_instance
aws_instance.foo.1:
  ID = bar
  foo = foo
  type = aws_instance
`

const testTerraformApplyCountDecToOneStr = `
aws_instance.foo:
  ID = bar
  foo = foo
  type = aws_instance
`

const testTerraformApplyCountDecToOneCorruptedStr = `
aws_instance.foo:
  ID = bar
  foo = foo
  type = aws_instance
`

const testTerraformApplyCountDecToOneCorruptedPlanStr = `
DIFF:

DESTROY: aws_instance.foo.0

STATE:

aws_instance.foo:
  ID = bar
  foo = foo
  type = aws_instance
aws_instance.foo.0:
  ID = baz
  type = aws_instance
`

const testTerraformApplyCountTaintedStr = `
<no state>
`

const testTerraformApplyCountVariableStr = `
aws_instance.foo.0:
  ID = foo
  foo = foo
  type = aws_instance
aws_instance.foo.1:
  ID = foo
  foo = foo
  type = aws_instance
`

const testTerraformApplyCountVariableRefStr = `
aws_instance.bar:
  ID = foo
  foo = 2
  type = aws_instance

  Dependencies:
    aws_instance.foo
aws_instance.foo.0:
  ID = foo
aws_instance.foo.1:
  ID = foo
`

const testTerraformApplyMinimalStr = `
aws_instance.bar:
  ID = foo
aws_instance.foo:
  ID = foo
`

const testTerraformApplyModuleStr = `
aws_instance.bar:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.foo:
  ID = foo
  num = 2
  type = aws_instance

module.child:
  aws_instance.baz:
    ID = foo
    foo = bar
    type = aws_instance
`

const testTerraformApplyModuleBoolStr = `
aws_instance.bar:
  ID = foo
  foo = 1
  type = aws_instance

  Dependencies:
    module.child

module.child:
  <no state>
  Outputs:

  leader = 1
`

const testTerraformApplyModuleDestroyOrderStr = `
<no state>
module.child:
  <no state>
`

const testTerraformApplyMultiProviderStr = `
aws_instance.bar:
  ID = foo
  foo = bar
  type = aws_instance
do_instance.foo:
  ID = foo
  num = 2
  type = do_instance
`

const testTerraformApplyModuleOnlyProviderStr = `
<no state>
module.child:
  aws_instance.foo:
    ID = foo
  test_instance.foo:
    ID = foo
`

const testTerraformApplyModuleProviderAliasStr = `
<no state>
module.child:
  aws_instance.foo:
    ID = foo
    provider = aws.eu
`

const testTerraformApplyOutputOrphanStr = `
<no state>
Outputs:

foo = bar
`

const testTerraformApplyProvisionerStr = `
aws_instance.bar:
  ID = foo

  Dependencies:
    aws_instance.foo
aws_instance.foo:
  ID = foo
  dynamical = computed_dynamical
  num = 2
  type = aws_instance
`

const testTerraformApplyProvisionerFailStr = `
aws_instance.bar: (tainted)
  ID = foo
aws_instance.foo:
  ID = foo
  num = 2
  type = aws_instance
`

const testTerraformApplyProvisionerFailCreateStr = `
aws_instance.bar: (tainted)
  ID = foo
`

const testTerraformApplyProvisionerFailCreateNoIdStr = `
<no state>
`

const testTerraformApplyProvisionerFailCreateBeforeDestroyStr = `
aws_instance.bar: (1 deposed)
  ID = bar
  require_new = abc
  Deposed ID 1 = foo (tainted)
`

const testTerraformApplyProvisionerResourceRefStr = `
aws_instance.bar:
  ID = foo
  num = 2
  type = aws_instance
`

const testTerraformApplyProvisionerSelfRefStr = `
aws_instance.foo:
  ID = foo
  foo = bar
  type = aws_instance
`

const testTerraformApplyProvisionerMultiSelfRefStr = `
aws_instance.foo.0:
  ID = foo
  foo = number 0
  type = aws_instance
aws_instance.foo.1:
  ID = foo
  foo = number 1
  type = aws_instance
aws_instance.foo.2:
  ID = foo
  foo = number 2
  type = aws_instance
`

const testTerraformApplyProvisionerDiffStr = `
aws_instance.bar:
  ID = foo
  foo = bar
  type = aws_instance
`

const testTerraformApplyDestroyStr = `
<no state>
`

const testTerraformApplyDestroyNestedModuleStr = `
module.child.subchild:
  <no state>
`

const testTerraformApplyErrorStr = `
aws_instance.bar:
  ID = bar

  Dependencies:
    aws_instance.foo
aws_instance.foo:
  ID = foo
  num = 2
`

const testTerraformApplyErrorCreateBeforeDestroyStr = `
aws_instance.bar:
  ID = bar
  require_new = abc
`

const testTerraformApplyErrorDestroyCreateBeforeDestroyStr = `
aws_instance.bar: (1 deposed)
  ID = foo
  Deposed ID 1 = bar
`

const testTerraformApplyErrorPartialStr = `
aws_instance.bar:
  ID = bar

  Dependencies:
    aws_instance.foo
aws_instance.foo:
  ID = foo
  num = 2
`

const testTerraformApplyTaintStr = `
aws_instance.bar:
  ID = foo
  num = 2
  type = aws_instance
`

const testTerraformApplyTaintDepStr = `
aws_instance.bar:
  ID = bar
  foo = foo
  num = 2
  type = aws_instance

  Dependencies:
    aws_instance.foo
aws_instance.foo:
  ID = foo
  num = 2
  type = aws_instance
`

const testTerraformApplyTaintDepRequireNewStr = `
aws_instance.bar:
  ID = foo
  foo = foo
  require_new = yes
  type = aws_instance

  Dependencies:
    aws_instance.foo
aws_instance.foo:
  ID = foo
  num = 2
  type = aws_instance
`

const testTerraformApplyOutputStr = `
aws_instance.bar:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.foo:
  ID = foo
  num = 2
  type = aws_instance

Outputs:

foo_num = 2
`

const testTerraformApplyOutputAddStr = `
aws_instance.test.0:
  ID = foo
  foo = foo0
  type = aws_instance
aws_instance.test.1:
  ID = foo
  foo = foo1
  type = aws_instance

Outputs:

firstOutput = foo0
secondOutput = foo1
`

const testTerraformApplyOutputListStr = `
aws_instance.bar.0:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.bar.1:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.bar.2:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.foo:
  ID = foo
  num = 2
  type = aws_instance

Outputs:

foo_num = [bar,bar,bar]
`

const testTerraformApplyOutputMultiStr = `
aws_instance.bar.0:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.bar.1:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.bar.2:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.foo:
  ID = foo
  num = 2
  type = aws_instance

Outputs:

foo_num = bar,bar,bar
`

const testTerraformApplyOutputMultiIndexStr = `
aws_instance.bar.0:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.bar.1:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.bar.2:
  ID = foo
  foo = bar
  type = aws_instance
aws_instance.foo:
  ID = foo
  num = 2
  type = aws_instance

Outputs:

foo_num = bar
`

const testTerraformApplyUnknownAttrStr = `
aws_instance.foo:
  ID = foo
  num = 2
  type = aws_instance
`

const testTerraformApplyVarsStr = `
aws_instance.bar:
  ID = foo
  bar = foo
  baz = override
  foo = us-west-2
  type = aws_instance
aws_instance.foo:
  ID = foo
  bar = baz
  list = Hello,World
  map = Baz,Foo,Hello
  num = 2
  type = aws_instance
`

const testTerraformApplyVarsEnvStr = `
aws_instance.bar:
  ID = foo
  bar = Hello,World
  baz = Baz,Foo,Hello
  foo = baz
  type = aws_instance
`

const testTerraformPlanStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "2"
  type: "" => "aws_instance"
CREATE: aws_instance.foo
  num:  "" => "2"
  type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanComputedStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "<computed>"
  type: "" => "aws_instance"
CREATE: aws_instance.foo
  foo:  "" => "<computed>"
  num:  "" => "2"
  type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanComputedIdStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "<computed>"
  type: "" => "aws_instance"
CREATE: aws_instance.foo
  foo:  "" => "<computed>"
  num:  "" => "2"
  type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanComputedListStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "<computed>"
  type: "" => "aws_instance"
CREATE: aws_instance.foo
  list.#: "" => "<computed>"
  num:    "" => "2"
  type:   "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanCountStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "foo,foo,foo,foo,foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.0
  foo:  "" => "foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.1
  foo:  "" => "foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.2
  foo:  "" => "foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.3
  foo:  "" => "foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.4
  foo:  "" => "foo"
  type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanCountIndexStr = `
DIFF:

CREATE: aws_instance.foo.0
  foo:  "" => "0"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.1
  foo:  "" => "1"
  type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanCountIndexZeroStr = `
DIFF:

CREATE: aws_instance.foo
  foo:  "" => "0"
  type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanCountOneIndexStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo
  foo:  "" => "foo"
  type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanCountZeroStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => ""
  type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanCountVarStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "foo,foo,foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.0
  foo:  "" => "foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.1
  foo:  "" => "foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.2
  foo:  "" => "foo"
  type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanCountDecreaseStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "bar"
  type: "" => "aws_instance"
DESTROY: aws_instance.foo.1
DESTROY: aws_instance.foo.2

STATE:

aws_instance.foo.0:
  ID = bar
  foo = foo
  type = aws_instance
aws_instance.foo.1:
  ID = bar
aws_instance.foo.2:
  ID = bar
`

const testTerraformPlanCountIncreaseStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "bar"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.1
  foo:  "" => "foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.2
  foo:  "" => "foo"
  type: "" => "aws_instance"

STATE:

aws_instance.foo:
  ID = bar
  foo = foo
  type = aws_instance
`

const testTerraformPlanCountIncreaseFromOneStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "bar"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.1
  foo:  "" => "foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.2
  foo:  "" => "foo"
  type: "" => "aws_instance"

STATE:

aws_instance.foo.0:
  ID = bar
  foo = foo
  type = aws_instance
`

const testTerraformPlanCountIncreaseFromOneCorruptedStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "bar"
  type: "" => "aws_instance"
DESTROY: aws_instance.foo
CREATE: aws_instance.foo.1
  foo:  "" => "foo"
  type: "" => "aws_instance"
CREATE: aws_instance.foo.2
  foo:  "" => "foo"
  type: "" => "aws_instance"

STATE:

aws_instance.foo:
  ID = bar
  foo = foo
  type = aws_instance
aws_instance.foo.0:
  ID = bar
  foo = foo
  type = aws_instance
`

const testTerraformPlanDestroyStr = `
DIFF:

DESTROY: aws_instance.one
DESTROY: aws_instance.two

STATE:

aws_instance.one:
  ID = bar
aws_instance.two:
  ID = baz
`

const testTerraformPlanDiffVarStr = `
DIFF:

CREATE: aws_instance.bar
  num:  "" => "3"
  type: "" => "aws_instance"
UPDATE: aws_instance.foo
  num: "2" => "3"

STATE:

aws_instance.foo:
  ID = bar
  num = 2
`

const testTerraformPlanEmptyStr = `
DIFF:

CREATE: aws_instance.bar
CREATE: aws_instance.foo

STATE:

<no state>
`

const testTerraformPlanEscapedVarStr = `
DIFF:

CREATE: aws_instance.foo
  foo:  "" => "bar-${baz}"
  type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanModulesStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "2"
  type: "" => "aws_instance"
CREATE: aws_instance.foo
  num:  "" => "2"
  type: "" => "aws_instance"

module.child:
  CREATE: aws_instance.foo
    num:  "" => "2"
    type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanModuleCycleStr = `
DIFF:

CREATE: aws_instance.b
CREATE: aws_instance.c
  some_input: "" => "<computed>"
  type:       "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanModuleDestroyStr = `
DIFF:

DESTROY: aws_instance.foo

module.child:
  DESTROY MODULE
  DESTROY: aws_instance.foo

STATE:

aws_instance.foo:
  ID = bar

module.child:
  aws_instance.foo:
    ID = bar
`

const testTerraformPlanModuleDestroyCycleStr = `
DIFF:

module.a_module:
  DESTROY MODULE
  DESTROY: aws_instance.a
module.b_module:
  DESTROY MODULE
  DESTROY: aws_instance.b

STATE:

module.a_module:
  aws_instance.a:
    ID = a
module.b_module:
  aws_instance.b:
    ID = b
`

const testTerraformPlanModuleDestroyMultivarStr = `
DIFF:

module.child:
  DESTROY MODULE
  DESTROY: aws_instance.foo.0
  DESTROY: aws_instance.foo.1

STATE:

<no state>
module.child:
  aws_instance.foo.0:
    ID = bar0
  aws_instance.foo.1:
    ID = bar1
`

const testTerraformPlanModuleInputStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "2"
  type: "" => "aws_instance"

module.child:
  CREATE: aws_instance.foo
    foo:  "" => "42"
    type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanModuleInputComputedStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "<computed>"
  type: "" => "aws_instance"

module.child:
  CREATE: aws_instance.foo
    foo:  "" => "<computed>"
    type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanModuleInputVarStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "2"
  type: "" => "aws_instance"

module.child:
  CREATE: aws_instance.foo
    foo:  "" => "52"
    type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanModuleMultiVarStr = `
DIFF:

CREATE: aws_instance.parent.0
CREATE: aws_instance.parent.1

module.child:
  CREATE: aws_instance.bar.0
    baz:  "" => "baz"
    type: "" => "aws_instance"
  CREATE: aws_instance.bar.1
    baz:  "" => "baz"
    type: "" => "aws_instance"
  CREATE: aws_instance.foo
    foo:  "" => "baz,baz"
    type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanModuleOrphansStr = `
DIFF:

CREATE: aws_instance.foo
  num:  "" => "2"
  type: "" => "aws_instance"

module.child:
  DESTROY: aws_instance.foo

STATE:

module.child:
  aws_instance.foo:
    ID = baz
`

const testTerraformPlanModuleVarStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "2"
  type: "" => "aws_instance"

module.child:
  CREATE: aws_instance.foo
    num:  "" => "2"
    type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanModuleVarComputedStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "<computed>"
  type: "" => "aws_instance"

module.child:
  CREATE: aws_instance.foo
    foo:  "" => "<computed>"
    type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanModuleVarIntStr = `
DIFF:

module.child:
  CREATE: aws_instance.foo
    num:  "" => "2"
    type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanOrphanStr = `
DIFF:

DESTROY: aws_instance.baz
CREATE: aws_instance.foo
  num:  "" => "2"
  type: "" => "aws_instance"

STATE:

aws_instance.baz:
  ID = bar
`

const testTerraformPlanStateStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "2"
  type: "" => "aws_instance"
UPDATE: aws_instance.foo
  num:  "" => "2"
  type: "" => "aws_instance"

STATE:

aws_instance.foo:
  ID = bar
`

const testTerraformPlanTaintStr = `
DIFF:

DESTROY/CREATE: aws_instance.bar
  foo:  "" => "2"
  type: "" => "aws_instance"

STATE:

aws_instance.bar: (tainted)
  ID = baz
aws_instance.foo:
  ID = bar
  num = 2
`

const testTerraformPlanMultipleTaintStr = `
DIFF:

DESTROY/CREATE: aws_instance.bar
  foo:  "" => "2"
  type: "" => "aws_instance"

STATE:

aws_instance.bar: (2 tainted)
  ID = <not created>
  Tainted ID 1 = baz
  Tainted ID 2 = zip
aws_instance.foo:
  ID = bar
  num = 2
`

const testTerraformPlanVarMultiCountOneStr = `
DIFF:

CREATE: aws_instance.bar
  foo:  "" => "2"
  type: "" => "aws_instance"
CREATE: aws_instance.foo
  num:  "" => "2"
  type: "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanPathVarStr = `
DIFF:

CREATE: aws_instance.foo
  cwd:    "" => "%s/barpath"
  module: "" => "%s/foopath"
  root:   "" => "%s/barpath"
  type:   "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanIgnoreChangesStr = `
DIFF:

UPDATE: aws_instance.foo
  type: "" => "aws_instance"

STATE:

aws_instance.foo:
  ID = bar
  ami = ami-abcd1234
`

const testTerraformPlanIgnoreChangesWildcardStr = `
DIFF:



STATE:

aws_instance.foo:
  ID = bar
  ami = ami-abcd1234
  instance_type = t2.micro
`

const testTerraformPlanComputedValueInMap = `
DIFF:

CREATE: aws_computed_source.intermediates
  computed_read_only: "" => "<computed>"

module.test_mod:
  CREATE: aws_instance.inner2
    looked_up: "" => "<computed>"
    type:      "" => "aws_instance"

STATE:

<no state>
`

const testTerraformPlanModuleVariableFromSplat = `
DIFF:

module.mod1:
  CREATE: aws_instance.test.0
    thing: "" => "doesnt"
    type:  "" => "aws_instance"
  CREATE: aws_instance.test.1
    thing: "" => "doesnt"
    type:  "" => "aws_instance"
module.mod2:
  CREATE: aws_instance.test.0
    thing: "" => "doesnt"
    type:  "" => "aws_instance"
  CREATE: aws_instance.test.1
    thing: "" => "doesnt"
    type:  "" => "aws_instance"

STATE:

<no state>`

const testTerraformInputHCL = `
hcl_instance.hcltest:
  ID = foo
  bar.w = z
  bar.x = y
  foo.# = 2
  foo.0 = a
  foo.1 = b
  type = hcl_instance
`
