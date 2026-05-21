// Health check — runs inline before React hydrates so the indicator
// resolves to Healthy / Unhealthy / Offline without waiting for JS bundles.
fetch("/api/v1/system/health").then(function(r){
  var el=document.getElementById("health-indicator");
  if(!el)return;
  if(r.ok){
    el.innerHTML='<span class="relative flex h-2 w-2"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-cp-green opacity-75"></span><span class="relative inline-flex rounded-full h-2 w-2 bg-cp-green"></span></span><span class="text-xs text-muted-foreground">Healthy</span>';
  } else {
    el.innerHTML='<span class="relative flex h-2 w-2"><span class="relative inline-flex rounded-full h-2 w-2 bg-cp-red"></span></span><span class="text-xs text-cp-red">Unhealthy</span>';
  }
}).catch(function(){
  var el=document.getElementById("health-indicator");
  if(el)el.innerHTML='<span class="relative flex h-2 w-2"><span class="relative inline-flex rounded-full h-2 w-2 bg-cp-red"></span></span><span class="text-xs text-cp-red">Offline</span>';
});
// Sign-out is now handled by the AccountMenu React component.
