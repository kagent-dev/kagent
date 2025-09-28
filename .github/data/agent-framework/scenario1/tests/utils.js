global = {
  sleep: ms => new Promise(resolve => setTimeout(resolve, ms)),
  waitOnFailedTest: (done, currentRetry) => {
    if(currentRetry > 0){
      process.stdout.write(".");
      setTimeout(done, 1000);
    } else {
      done();
    }
  }
};

module.exports = global;