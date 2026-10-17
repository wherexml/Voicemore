/* Deform and relight the ribbon material itself. No detached line overlays. */
(() => {
  const canvas = document.getElementById('ribbon-flow');
  const gl = canvas.getContext('webgl', { alpha: false, antialias: false, depth: false, powerPreference: 'low-power' });
  if (!gl) { canvas.style.display = 'none'; return; }
  const vertex = `attribute vec2 position; varying vec2 uv;
    void main(){uv=position*.5+.5;gl_Position=vec4(position,0.,1.);}`;
  const fragment = `precision highp float;
    varying vec2 uv; uniform sampler2D material; uniform float time;
    void main(){
      vec2 p=vec2(uv.x,1.-uv.y);
      float t=time;
      // Long, overlapping waves move entire folds with smooth, non-repeating phases.
      float a=sin(p.y*6.1+p.x*2.7-t*.31);
      float b=sin(p.x*7.3-p.y*3.2+t*.23);
      float c=sin((p.x+p.y)*4.2-t*.17);
      vec2 bend=vec2(a*.013+c*.006,b*.012-c*.005);
      // Clamp gently at the frame; no stretching of exposed image edges.
      vec2 edge=smoothstep(vec2(0.),vec2(.16),p)*smoothstep(vec2(0.),vec2(.16),1.-p);
      vec2 q=clamp(p+bend*edge,0.,1.);
      vec3 color=texture2D(material,q).rgb;
      float luminance=dot(color,vec3(.2126,.7152,.0722));
      float ribbon=smoothstep(.055,.38,luminance);
      // Traveling illumination follows the same deformed material coordinates.
      float light=.5+.5*sin(q.x*6.0+q.y*8.0-t*.43+.55*sin(q.y*4.-t*.19));
      float secondary=.5+.5*sin(q.x*9.0-q.y*4.0+t*.29);
      float gain=.88+.30*light+.12*secondary;
      color*=mix(1.,gain,ribbon);
      color+=vec3(.035,.065,.09)*pow(light,4.)*ribbon*.48;
      // Gentle tonal separation preserves silk/glass translucency without hard neon edges.
      gl_FragColor=vec4(color,1.);
    }`;
  function compile(type, source) {
    const shader = gl.createShader(type);
    gl.shaderSource(shader, source); gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(shader));
    return shader;
  }
  try {
    const program = gl.createProgram();
    gl.attachShader(program, compile(gl.VERTEX_SHADER, vertex));
    gl.attachShader(program, compile(gl.FRAGMENT_SHADER, fragment));
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error('Ribbon shader link failed');
    gl.useProgram(program);
    const buffer = gl.createBuffer(); gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1,-1, 1,-1, -1,1, -1,1, 1,-1, 1,1]), gl.STATIC_DRAW);
    const position = gl.getAttribLocation(program, 'position');
    gl.enableVertexAttribArray(position); gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0);
    const clock = gl.getUniformLocation(program, 'time');
    const texture = gl.createTexture(); gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    const reduced = matchMedia('(prefers-reduced-motion: reduce)');
    let ready=false, frame=0, previous=0, elapsed=0, lost=false;
    function resize() {
      const rect=canvas.getBoundingClientRect(), density=Math.min(devicePixelRatio||1,2);
      const width=Math.round(rect.width*density), height=Math.round(rect.height*density);
      if(canvas.width!==width||canvas.height!==height){canvas.width=width;canvas.height=height;gl.viewport(0,0,width,height);}
    }
    function draw(){if(!ready||lost)return;resize();gl.uniform1f(clock,reduced.matches?0:elapsed);gl.drawArrays(gl.TRIANGLES,0,6);}
    function tick(now){
      frame=0;if(document.hidden||reduced.matches||lost)return;
      // Cap to 30 fps; accumulated time stops when hidden, avoiding return-to-tab jumps.
      if(!previous)previous=now;
      if(now-previous>=1000/30){elapsed+=(now-previous)/1000;previous=now;draw();}
      frame=requestAnimationFrame(tick);
    }
    function sync(){cancelAnimationFrame(frame);frame=0;previous=0;draw();if(ready&&!document.hidden&&!reduced.matches&&!lost)frame=requestAnimationFrame(tick);}
    new ResizeObserver(()=>draw()).observe(canvas);
    document.addEventListener('visibilitychange',sync);reduced.addEventListener('change',sync);
    canvas.addEventListener('webglcontextlost',()=>{lost=true;cancelAnimationFrame(frame);canvas.style.display='none';});
    const image=new Image();
    image.onload=()=>{gl.bindTexture(gl.TEXTURE_2D,texture);gl.texImage2D(gl.TEXTURE_2D,0,gl.RGB,gl.RGB,gl.UNSIGNED_BYTE,image);ready=true;canvas.dataset.renderer='material-flow';sync();};
    image.onerror=()=>{canvas.style.display='none';};
    image.src=new URL('aurora-flow.png',document.currentScript.src).href;
  } catch(error) {
    canvas.style.display='none';console.warn('Ribbon flow uses static fallback:',error.message);
  }
})();
