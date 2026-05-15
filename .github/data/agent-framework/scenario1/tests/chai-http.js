const chaiHttp = require("chai-http");
const chai = require("chai");
const expect = chai.expect;
chai.use(chaiHttp);
const utils = require('./utils');
const fs = require("fs");
const { debugLog } = require('./utils/logging');

process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';
process.env.NODE_NO_WARNINGS = 1;
chai.config.truncateThreshold = 4000; // length threshold for actual and expected values in assertion errors

global = {
  checkURL: ({ host, path = "", headers = [], certFile = '', keyFile = '', retCode }) => {
    debugLog(`Checking URL: ${host}${path} with expected return code: ${retCode}`);

    let cert = certFile ? fs.readFileSync(certFile) : '';
    let key = keyFile ? fs.readFileSync(keyFile) : '';
    let request = chai.request(host).head(path).redirects(0).cert(cert).key(key);

    debugLog(`Setting headers: ${JSON.stringify(headers)}`);
    headers.forEach(header => request.set(header.key, header.value));

    return request
      .send()
      .then(async function (res) {
        debugLog(`Response status code: ${res.status}`);
        expect(res).to.have.property('status', retCode);
      });
  },

  checkURLWithIP: ({ ip, host, protocol = "http", path = "", headers = [], certFile = '', keyFile = '', retCode }) => {
    debugLog(`Checking URL with IP: ${ip}, Host: ${host}, Path: ${path} with expected return code: ${retCode}`);

    let cert = certFile ? fs.readFileSync(certFile) : '';
    let key = keyFile ? fs.readFileSync(keyFile) : '';

    let url = `${protocol}://${ip}`;

    // Use chai-http to make a request to the IP address, but set the Host header
    let request = chai.request(url).head(path).redirects(0).cert(cert).key(key).set('Host', host);

    debugLog(`Setting headers: ${JSON.stringify(headers)}`);
    headers.forEach(header => request.set(header.key, header.value));

    return request
      .send()
      .then(async function (res) {
        debugLog(`Response status code: ${res.status}`);
        debugLog(`Response ${JSON.stringify(res)}`);
        expect(res).to.have.property('status', retCode);
      });
  },

  checkBody: ({ host, path = "", headers = [], body = '', certFile = '', keyFile = '', method = "get", data = "", match = true }) => {
    debugLog(`Checking body at ${host}${path} with method: ${method} and match condition: ${match}`);

    let cert = certFile ? fs.readFileSync(certFile) : '';
    let key = keyFile ? fs.readFileSync(keyFile) : '';
    let request = chai.request(host);

    switch (method) {
      case "get":
        request = request.get(path).redirects(0).cert(cert).key(key);
        break;
      case "post":
        request = request.post(path).redirects(0);
        break;
      case "put":
        request = request.put(path).redirects(0);
        break;
      case "head":
        request = request.head(path).redirects(0);
        break;
      default:
        throw 'The requested method is not implemented.';
    }

    debugLog(`Setting headers: ${JSON.stringify(headers)}`);
    headers.forEach(header => request.set(header.key, header.value));

    debugLog(`Sending data: ${data}`);
    return request
      .send(data)
      .then(async function (res) {
        debugLog(`Response body: ${res.text}`);
        if (match) {
          expect(res.text).to.contain(body);
        } else {
          expect(res.text).not.to.contain(body);
        }
      });
  },

  checkHeaders: ({ host, path = "", headers = [], certFile = '', keyFile = '', expectedHeaders = [] }) => {
    debugLog(`Checking headers for URL: ${host}${path}`);

    let cert = certFile ? fs.readFileSync(certFile) : '';
    let key = keyFile ? fs.readFileSync(keyFile) : '';
    let request = chai.request(host).get(path).redirects(0).cert(cert).key(key);

    debugLog(`Setting headers: ${JSON.stringify(headers)}`);
    headers.forEach(header => request.set(header.key, header.value));

    return request
      .send()
      .then(async function (res) {
        debugLog(`Response headers: ${JSON.stringify(res.header)}`);
        expectedHeaders.forEach(header => {
          debugLog(`Checking header ${header.key} with expected value: ${header.value}`);
          if (header.value === '*') {
            expect(res.header).to.have.property(header.key);
          } else {
            expect(res.header[header.key]).to.equal(header.value);
          }
        });
      });
  },

  checkWithMethod: ({ host, path, headers = [], method = "get", certFile = '', keyFile = '', retCode }) => {
    debugLog(`Checking URL: ${host}${path} with method: ${method} and expected return code: ${retCode}`);

    let cert = certFile ? fs.readFileSync(certFile) : '';
    let key = keyFile ? fs.readFileSync(keyFile) : '';
    let request = chai.request(host);

    switch (method) {
      case 'get':
        request = request.get(path);
        break;
      case 'post':
        request = request.post(path);
        break;
      case 'put':
        request = request.put(path);
        break;
      default:
        throw 'The requested method is not implemented.';
    }

    request.cert(cert).key(key).redirects(0);

    debugLog(`Setting headers: ${JSON.stringify(headers)}`);
    headers.forEach(header => request.set(header.key, header.value));

    return request
      .send()
      .then(async function (res) {
        debugLog(`Response status code: ${res.status}`);
        expect(res).to.have.property('status', retCode);
      });
  }
};

module.exports = global;

afterEach(function (done) {
  if (this.currentTest.currentRetry() > 0 && this.currentTest.currentRetry() % 5 === 0) {
    console.log(`Test "${this.currentTest.fullTitle()}" retry: ${this.currentTest.currentRetry()}`);
  }
  utils.waitOnFailedTest(done, this.currentTest.currentRetry());
});
