const jsYaml = require('js-yaml');
const deepObjectDiff = require('deep-object-diff');
const chaiExec = require("@jsdevtools/chai-exec");
const chai = require("chai");
const expect = chai.expect;
const should = chai.should();
chai.use(chaiExec);
const utils = require('./utils');
const { debugLog } = require('./utils/logging');
chai.config.truncateThreshold = 4000; // length threshold for actual and expected values in assertion errors

global = {
  checkKubernetesObject: async ({ context, namespace, kind, k8sObj, yaml }) => {
    let command = "kubectl --context " + context + " -n " + namespace + " get " + kind + " " + k8sObj + " -o json";
    debugLog(`Executing command: ${command}`);
    let cli = chaiExec(command);
    let json = jsYaml.load(yaml)

    debugLog(`Command output (stdout): ${cli.stdout}`);
    debugLog(`Command error (stderr): ${cli.stderr}`);

    cli.should.exit.with.code(0);
    cli.stderr.should.be.empty;
    let data = JSON.parse(cli.stdout);
    debugLog(`Parsed data from CLI: ${JSON.stringify(data)}`);

    let diff = deepObjectDiff.detailedDiff(json, data);
    debugLog(`Diff between expected and actual object: ${JSON.stringify(diff)}`);

    let expectedObject = false;
    if (Object.keys(diff.updated).length === 0 && Object.keys(diff.deleted).length === 0) {
      expectedObject = true;
    }
    debugLog(`Expected object found: ${expectedObject}`);
    expect(expectedObject, "The following object can't be found or is not as expected:\n" + yaml).to.be.true;
  },

  checkDeployment: async ({ context, namespace, k8sObj }) => {
    let command = "kubectl --context " + context + " -n " + namespace + " get deploy " + k8sObj + " -o jsonpath='{.status}'";
    debugLog(`Executing command: ${command}`);
    let cli = chaiExec(command);

    debugLog(`Command output (stdout): ${cli.stdout}`);
    debugLog(`Command error (stderr): ${cli.stderr}`);

    cli.stderr.should.be.empty;
    let readyReplicas = JSON.parse(cli.stdout.slice(1, -1)).readyReplicas || 0;
    let replicas = JSON.parse(cli.stdout.slice(1, -1)).replicas;
    debugLog(`Ready replicas: ${readyReplicas}, Total replicas: ${replicas}`);

    if (readyReplicas != replicas) {
      debugLog(`Deployment ${k8sObj} in ${context} not ready, retrying...`);
      await utils.sleep(1000);
    }
    cli.should.exit.with.code(0);
    readyReplicas.should.equal(replicas);
  },

  checkDeploymentHasPod: async ({ context, namespace, deployment }) => {
    let command = "kubectl --context " + context + " -n " + namespace + " get deploy " + deployment + " -o name'";
    debugLog(`Executing command: ${command}`);
    let cli = chaiExec(command);

    debugLog(`Command output (stdout): ${cli.stdout}`);
    debugLog(`Command error (stderr): ${cli.stderr}`);

    cli.stderr.should.be.empty;
    cli.stdout.should.not.be.empty;
    cli.stdout.should.contain(deployment);
  },

  checkDeploymentsWithLabels: async ({ context, namespace, labels, instances }) => {
    let command = "kubectl --context " + context + " -n " + namespace + " get deploy -l " + labels + " -o jsonpath='{.items}'";
    debugLog(`Executing command: ${command}`);
    let cli = chaiExec(command);

    debugLog(`Command output (stdout): ${cli.stdout}`);
    debugLog(`Command error (stderr): ${cli.stderr}`);

    cli.stderr.should.be.empty;
    let deployments = JSON.parse(cli.stdout.slice(1, -1));
    debugLog(`Found deployments: ${JSON.stringify(deployments)}`);

    expect(deployments).to.have.lengthOf(instances);
    deployments.forEach((deployment) => {
      let readyReplicas = deployment.status.readyReplicas || 0;
      let replicas = deployment.status.replicas;
      debugLog(`Deployment ${deployment.metadata.name} - Ready replicas: ${readyReplicas}, Total replicas: ${replicas}`);

      if (readyReplicas != replicas) {
        debugLog(`Deployment ${deployment.metadata.name} in ${context} not ready, retrying...`);
        utils.sleep(1000);
      }
      cli.should.exit.with.code(0);
      readyReplicas.should.equal(replicas);
    });
  },

  checkStatefulSet: async ({ context, namespace, k8sObj }) => {
    let command = "kubectl --context " + context + " -n " + namespace + " get sts " + k8sObj + " -o jsonpath='{.status}'";
    debugLog(`Executing command: ${command}`);
    let cli = chaiExec(command);

    debugLog(`Command output (stdout): ${cli.stdout}`);
    debugLog(`Command error (stderr): ${cli.stderr}`);

    cli.stderr.should.be.empty;
    let readyReplicas = JSON.parse(cli.stdout.slice(1, -1)).readyReplicas || 0;
    let replicas = JSON.parse(cli.stdout.slice(1, -1)).replicas;
    debugLog(`StatefulSet ${k8sObj} - Ready replicas: ${readyReplicas}, Total replicas: ${replicas}`);

    if (readyReplicas != replicas) {
      debugLog(`StatefulSet ${k8sObj} in ${context} not ready, retrying...`);
      await utils.sleep(1000);
    }
    cli.should.exit.with.code(0);
    readyReplicas.should.equal(replicas);
  },

  checkDaemonSet: async ({ context, namespace, k8sObj }) => {
    let command = "kubectl --context " + context + " -n " + namespace + " get ds " + k8sObj + " -o jsonpath='{.status}'";
    debugLog(`Executing command: ${command}`);
    let cli = chaiExec(command);

    debugLog(`Command output (stdout): ${cli.stdout}`);
    debugLog(`Command error (stderr): ${cli.stderr}`);

    cli.stderr.should.be.empty;
    let readyReplicas = JSON.parse(cli.stdout.slice(1, -1)).numberReady || 0;
    let replicas = JSON.parse(cli.stdout.slice(1, -1)).desiredNumberScheduled;
    debugLog(`DaemonSet ${k8sObj} - Ready replicas: ${readyReplicas}, Total replicas: ${replicas}`);

    if (readyReplicas != replicas) {
      debugLog(`DaemonSet ${k8sObj} in ${context} not ready, retrying...`);
      await utils.sleep(1000);
    }
    cli.should.exit.with.code(0);
    readyReplicas.should.equal(replicas);
  },

  k8sObjectIsPresent: ({ context, namespace, k8sType, k8sObj }) => {
    // covers both namespace scoped and cluster scoped objects
    let command = "kubectl --context " + context + " get " + k8sType + " " + k8sObj + " -o name";
    if (namespace) {
      command = "kubectl --context " + context + " -n " + namespace + " get " + k8sType + " " + k8sObj + " -o name";
    }
    debugLog(`Executing command: ${command}`);
    let cli = chaiExec(command);

    debugLog(`Command output (stdout): ${cli.stdout}`);
    debugLog(`Command error (stderr): ${cli.stderr}`);

    cli.stderr.should.be.empty;
    cli.should.exit.with.code(0);
  },

  genericCommand: async ({ command, responseContains = "" }) => {
    debugLog(`Executing generic command: ${command}`);
    let cli = chaiExec(command);

    if (cli.stderr && cli.stderr != "") {
      debugLog(`Command ${command} not successful: ${cli.stderr}`);
      await utils.sleep(1000);
    }

    debugLog(`Command output (stdout): ${cli.stdout}`);
    debugLog(`Command error (stderr): ${cli.stderr}`);

    cli.stderr.should.be.empty;
    cli.should.exit.with.code(0);
    if (responseContains != "") {
      debugLog(`Checking if stdout contains: ${responseContains}`);
      cli.stdout.should.contain(responseContains);
    }
  },

  getOutputForCommand: ({ command }) => {
    debugLog(`Executing command: ${command}`);
    let cli = chaiExec(command);
    debugLog(`Command output (stdout): ${cli.stdout}`);
    return cli.stdout;
  },
  curlInPod: ({ curlCommand, podName, namespace }) => {
    debugLog(`Executing curl command: ${curlCommand} on pod: ${podName} in namespace: ${namespace}`);
    const cli = chaiExec(curlCommand);
    debugLog(`Curl command output (stdout): ${cli.stdout}`);
    return cli.stdout;
  },
  curlInDeployment: async ({ curlCommand, deploymentName, namespace, context }) => {
    debugLog(`Executing curl command: ${curlCommand} on deployment: ${deploymentName} in namespace: ${namespace} and context: ${context}`);
    let getPodCommand = `kubectl --context ${context} -n ${namespace} get pods -l app=${deploymentName} -o jsonpath='{.items[0].metadata.name}'`;
    let podName = chaiExec(getPodCommand).stdout.trim();
    debugLog(`Pod selected for curl command: ${podName}`);
    let execCommand = `kubectl --context ${context} -n ${namespace} exec ${podName} -- ${curlCommand}`;
    const cli = chaiExec(execCommand);
    debugLog(`Curl command output (stdout): ${cli.stdout}`);
    return cli.stdout;
  },
};

module.exports = global;

afterEach(function (done) {
  if (this.currentTest.currentRetry() > 0 && this.currentTest.currentRetry() % 5 === 0) {
    debugLog(`Test "${this.currentTest.fullTitle()}" retry: ${this.currentTest.currentRetry()}`);
  }
  utils.waitOnFailedTest(done, this.currentTest.currentRetry())
});
