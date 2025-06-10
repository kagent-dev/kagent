const helpers = require('./tests/chai-exec');

describe("Application mysql, version v1", () => {
  let cluster = process.env.CLUSTER_CTX;
  it('mysql-v1 pods are ready in ' + cluster, () => helpers.checkDeployment({ 
    context: cluster, 
    namespace: 'default', 
    k8sObj: 'mysql-v1' }));
  it('mysql-v1 service is present in ' + cluster, () => helpers.k8sObjectIsPresent({ 
    context: cluster, 
    namespace: 'default', 
    k8sType: 'service', 
    k8sObj: 'mysql-v1' }));

});

describe("Application neo4j-db, version v1", () => {
  let cluster = process.env.CLUSTER_CTX;
  it('neo4j-db-v1 pods are ready in ' + cluster, () => helpers.checkDeployment({ 
    context: cluster, 
    namespace: 'default', 
    k8sObj: 'neo4j-db-v1' }));
  it('neo4j-db-v1 service is present in ' + cluster, () => helpers.k8sObjectIsPresent({ 
    context: cluster, 
    namespace: 'default', 
    k8sType: 'service', 
    k8sObj: 'neo4j-db-v1' }));

});

describe("Application backend, version v1", () => {
  let cluster = process.env.CLUSTER_CTX;
  it('backend-v1 pods are ready in ' + cluster, () => helpers.checkDeployment({ 
    context: cluster, 
    namespace: 'default', 
    k8sObj: 'backend-v1' }));
  it('backend-v1 service is present in ' + cluster, () => helpers.k8sObjectIsPresent({ 
    context: cluster, 
    namespace: 'default', 
    k8sType: 'service', 
    k8sObj: 'backend-v1' }));

});

describe("Application backend, version v2", () => {
  let cluster = process.env.CLUSTER_CTX;
  it('backend-v2 pods are ready in ' + cluster, () => helpers.checkDeployment({ 
    context: cluster, 
    namespace: 'default', 
    k8sObj: 'backend-v2' }));
  it('backend-v2 service is present in ' + cluster, () => helpers.k8sObjectIsPresent({ 
    context: cluster, 
    namespace: 'default', 
    k8sType: 'service', 
    k8sObj: 'backend-v2' }));
  it('backend-v2 is able to call http://neo4j-db-v1:7474', () => helpers.genericCommand({ 
    command: `kubectl --context ${cluster} -n default exec deploy/backend-v2 -- curl -s -o /dev/null -w "%{http_code}" http://neo4j-db-v1:7474`, 
    responseContains: "200" }));

});

describe("Application backend, version v3", () => {
  let cluster = process.env.CLUSTER_CTX;
  it('backend-v3 pods are ready in ' + cluster, () => helpers.checkDeployment({ 
    context: cluster, 
    namespace: 'default', 
    k8sObj: 'backend-v3' }));
  it('backend-v3 service is present in ' + cluster, () => helpers.k8sObjectIsPresent({ 
    context: cluster, 
    namespace: 'default', 
    k8sType: 'service', 
    k8sObj: 'backend-v3' }));

});

describe("Application frontend, version v1", () => {
  let cluster = process.env.CLUSTER_CTX;
  it('frontend-v1 pods are ready in ' + cluster, () => helpers.checkDeployment({ 
    context: cluster, 
    namespace: 'default', 
    k8sObj: 'frontend-v1' }));
  it('frontend-v1 service is present in ' + cluster, () => helpers.k8sObjectIsPresent({ 
    context: cluster, 
    namespace: 'default', 
    k8sType: 'service', 
    k8sObj: 'frontend-v1' }));
  it('frontend-v1 is able to call http://backend-v1:8080', () => helpers.genericCommand({ 
    command: `kubectl --context ${cluster} -n default exec deploy/frontend-v1 -- curl -s -o /dev/null -w "%{http_code}" http://backend-v1:8080`, 
    responseContains: "200" }));
  it('frontend-v1 is able to call http://backend-v2:8080', () => helpers.genericCommand({ 
    command: `kubectl --context ${cluster} -n default exec deploy/frontend-v1 -- curl -s -o /dev/null -w "%{http_code}" http://backend-v2:8080`, 
    responseContains: "200" }));
  it('frontend-v1 is able to call http://backend-v3:8080', () => helpers.genericCommand({ 
    command: `kubectl --context ${cluster} -n default exec deploy/frontend-v1 -- curl -s -o /dev/null -w "%{http_code}" http://backend-v3:8080`, 
    responseContains: "200" }));

});
