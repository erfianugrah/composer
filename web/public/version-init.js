fetch("/api/v1/system/health").then(function(r){return r.json()}).then(function(d){
  var el=document.getElementById("composer-version");
  if(el&&d.version)el.textContent="Composer v"+d.version;
}).catch(function(){});
