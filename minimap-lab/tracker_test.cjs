const {test} = require('node:test');
const assert = require('node:assert/strict');
const {MinimapTracker, minimapNextDelay} = require('./web/tracker.js');
const success={found:true,position:{x:32958,y:32077,z:7},zoom:1,mode:'local',match_ms:3};

test('radius grows with elapsed capture time and rejects mismatched calibration',()=>{
  const t=new MinimapTracker();t.observe(success,100,105,5);
  assert.equal(t.hint(200,7,1).radius,5);
  assert.equal(t.hint(1100,7,1).radius,22);
  assert.equal(t.hint(5000,7,1).radius,64);
  assert.equal(t.hint(200,8,1).near.z,7);assert.equal(t.hint(200,9,1),null);assert.equal(t.hint(200,7,2),null);
  assert.equal(t.hint(40000,7,1),null);
});
test('global acquisition age is preserved and followed by wider local confirmation',()=>{
  const t=new MinimapTracker();t.observe({...success,mode:'global'},0,13000,13000);
  assert.equal(t.hint(13000,7,1).radius,64);assert.equal(t.stats(13000).ageMS,13000);
  t.observe(success,13100,13105,5);assert.equal(t.hint(13200,7,1).radius,5);
});
test('cadence subtracts the request duration from each sampling period',()=>{
  assert.equal(minimapNextDelay(100,130,10),70);
  assert.equal(minimapNextDelay(100,130,5),170);
  assert.equal(minimapNextDelay(100,350,10),0);
});
