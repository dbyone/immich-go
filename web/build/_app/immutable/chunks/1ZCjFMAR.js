import{i as e,n as t,r as n}from"./DsOoolsL.js";var r={MEDIA_PLAY_REQUEST:`mediaplayrequest`,MEDIA_PAUSE_REQUEST:`mediapauserequest`,MEDIA_MUTE_REQUEST:`mediamuterequest`,MEDIA_UNMUTE_REQUEST:`mediaunmuterequest`,MEDIA_LOOP_REQUEST:`medialooprequest`,MEDIA_VOLUME_REQUEST:`mediavolumerequest`,MEDIA_SEEK_REQUEST:`mediaseekrequest`,MEDIA_AIRPLAY_REQUEST:`mediaairplayrequest`,MEDIA_ENTER_FULLSCREEN_REQUEST:`mediaenterfullscreenrequest`,MEDIA_EXIT_FULLSCREEN_REQUEST:`mediaexitfullscreenrequest`,MEDIA_PREVIEW_REQUEST:`mediapreviewrequest`,MEDIA_ENTER_PIP_REQUEST:`mediaenterpiprequest`,MEDIA_EXIT_PIP_REQUEST:`mediaexitpiprequest`,MEDIA_ENTER_CAST_REQUEST:`mediaentercastrequest`,MEDIA_EXIT_CAST_REQUEST:`mediaexitcastrequest`,MEDIA_SHOW_TEXT_TRACKS_REQUEST:`mediashowtexttracksrequest`,MEDIA_HIDE_TEXT_TRACKS_REQUEST:`mediahidetexttracksrequest`,MEDIA_SHOW_SUBTITLES_REQUEST:`mediashowsubtitlesrequest`,MEDIA_DISABLE_SUBTITLES_REQUEST:`mediadisablesubtitlesrequest`,MEDIA_TOGGLE_SUBTITLES_REQUEST:`mediatogglesubtitlesrequest`,MEDIA_PLAYBACK_RATE_REQUEST:`mediaplaybackraterequest`,MEDIA_RENDITION_REQUEST:`mediarenditionrequest`,MEDIA_AUDIO_TRACK_REQUEST:`mediaaudiotrackrequest`,MEDIA_SEEK_TO_LIVE_REQUEST:`mediaseektoliverequest`,REGISTER_MEDIA_STATE_RECEIVER:`registermediastatereceiver`,UNREGISTER_MEDIA_STATE_RECEIVER:`unregistermediastatereceiver`},i={MEDIA_CHROME_ATTRIBUTES:`mediachromeattributes`,MEDIA_CONTROLLER:`mediacontroller`},a={MEDIA_AIRPLAY_UNAVAILABLE:`mediaAirplayUnavailable`,MEDIA_AUDIO_TRACK_ENABLED:`mediaAudioTrackEnabled`,MEDIA_AUDIO_TRACK_LIST:`mediaAudioTrackList`,MEDIA_AUDIO_TRACK_UNAVAILABLE:`mediaAudioTrackUnavailable`,MEDIA_BUFFERED:`mediaBuffered`,MEDIA_CAST_UNAVAILABLE:`mediaCastUnavailable`,MEDIA_CHAPTERS_CUES:`mediaChaptersCues`,MEDIA_CURRENT_TIME:`mediaCurrentTime`,MEDIA_DURATION:`mediaDuration`,MEDIA_ENDED:`mediaEnded`,MEDIA_ERROR:`mediaError`,MEDIA_ERROR_CODE:`mediaErrorCode`,MEDIA_ERROR_MESSAGE:`mediaErrorMessage`,MEDIA_FULLSCREEN_UNAVAILABLE:`mediaFullscreenUnavailable`,MEDIA_HAS_PLAYED:`mediaHasPlayed`,MEDIA_HEIGHT:`mediaHeight`,MEDIA_IS_AIRPLAYING:`mediaIsAirplaying`,MEDIA_IS_CASTING:`mediaIsCasting`,MEDIA_IS_FULLSCREEN:`mediaIsFullscreen`,MEDIA_IS_PIP:`mediaIsPip`,MEDIA_LOADING:`mediaLoading`,MEDIA_MUTED:`mediaMuted`,MEDIA_LOOP:`mediaLoop`,MEDIA_PAUSED:`mediaPaused`,MEDIA_PIP_UNAVAILABLE:`mediaPipUnavailable`,MEDIA_PLAYBACK_RATE:`mediaPlaybackRate`,MEDIA_PREVIEW_CHAPTER:`mediaPreviewChapter`,MEDIA_PREVIEW_COORDS:`mediaPreviewCoords`,MEDIA_PREVIEW_IMAGE:`mediaPreviewImage`,MEDIA_PREVIEW_TIME:`mediaPreviewTime`,MEDIA_RENDITION_LIST:`mediaRenditionList`,MEDIA_RENDITION_SELECTED:`mediaRenditionSelected`,MEDIA_RENDITION_UNAVAILABLE:`mediaRenditionUnavailable`,MEDIA_SEEKABLE:`mediaSeekable`,MEDIA_STREAM_TYPE:`mediaStreamType`,MEDIA_SUBTITLES_LIST:`mediaSubtitlesList`,MEDIA_SUBTITLES_SHOWING:`mediaSubtitlesShowing`,MEDIA_TARGET_LIVE_WINDOW:`mediaTargetLiveWindow`,MEDIA_TIME_IS_LIVE:`mediaTimeIsLive`,MEDIA_VOLUME:`mediaVolume`,MEDIA_VOLUME_LEVEL:`mediaVolumeLevel`,MEDIA_VOLUME_UNAVAILABLE:`mediaVolumeUnavailable`,MEDIA_LANG:`mediaLang`,MEDIA_WIDTH:`mediaWidth`},o=Object.entries(a),s=o.reduce((e,[t,n])=>(e[t]=n.toLowerCase(),e),{}),c=o.reduce((e,[t,n])=>(e[t]=n.toLowerCase(),e),{USER_INACTIVE_CHANGE:`userinactivechange`,BREAKPOINTS_CHANGE:`breakpointchange`,BREAKPOINTS_COMPUTED:`breakpointscomputed`});Object.entries(c).reduce((e,[t,n])=>{let r=s[t];return r&&(e[n]=r),e},{userinactivechange:`userinactive`});var l=Object.entries(s).reduce((e,[t,n])=>{let r=c[t];return r&&(e[n]=r),e},{userinactive:`userinactivechange`}),u={SUBTITLES:`subtitles`,CAPTIONS:`captions`,DESCRIPTIONS:`descriptions`,CHAPTERS:`chapters`,METADATA:`metadata`},d={DISABLED:`disabled`,HIDDEN:`hidden`,SHOWING:`showing`},f={MOUSE:`mouse`,PEN:`pen`,TOUCH:`touch`},p={UNAVAILABLE:`unavailable`,UNSUPPORTED:`unsupported`},m={LIVE:`live`,ON_DEMAND:`on-demand`,UNKNOWN:`unknown`},h={INLINE:`inline`,FULLSCREEN:`fullscreen`,PICTURE_IN_PICTURE:`picture-in-picture`};function g(e){let t={};for(let n of e)t[n.name]=n.value;return t}function ee(e){return te(e)??_(e,`media-controller`)}function te(e){let{MEDIA_CONTROLLER:t}=i,n=e.getAttribute(t);if(n)return ie(e)?.getElementById(n)}var ne=(e,t)=>!e||!t?!1:e?.contains(t)?!0:ne(e,t.getRootNode().host),_=(e,t)=>e?e.closest(t)||_(e.getRootNode().host,t):null;function re(e=document){let t=e?.activeElement;return t?re(t.shadowRoot)??t:null}function ie(e){let t=(e?.getRootNode)?.call(e);return t instanceof ShadowRoot||t instanceof Document?t:null}function ae(e,{depth:t=3,checkOpacity:n=!0,checkVisibilityCSS:r=!0}={}){if(e.checkVisibility)return e.checkVisibility({checkOpacity:n,checkVisibilityCSS:r});let i=e;for(;i&&t>0;){let e=getComputedStyle(i);if(n&&e.opacity===`0`||r&&e.visibility===`hidden`||e.display===`none`)return!1;i=i.parentElement,t--}return!0}function oe(e,t,n,r){let i=r.x-n.x,a=r.y-n.y,o=i*i+a*a;if(o===0)return 0;let s=((e-n.x)*i+(t-n.y)*a)/o;return Math.max(0,Math.min(1,s))}function v(e,t){return se(e,e=>e===t)||ce(e,t)}function se(e,t){let n;for(n of e.querySelectorAll(`style:not([media])`)??[]){let e;try{e=n.sheet?.cssRules}catch{continue}for(let n of e??[])if(t(n.selectorText))return n}}function ce(e,t){let n=e.querySelectorAll(`style:not([media])`)??[],r=n?.[n.length-1];if(!r?.sheet)return console.warn(`Media Chrome: No style sheet found on style tag of`,e),{style:{setProperty:()=>{},removeProperty:()=>``,getPropertyValue:()=>``}};let i=r?.sheet.insertRule(`${t}{}`,r.sheet.cssRules.length);return r.sheet.cssRules?.[i]}function le(e,t,n=NaN){let r=e.getAttribute(t);return r==null?n:+r}function ue(e,t,n){let r=+n;if(n==null||Number.isNaN(r)){e.hasAttribute(t)&&e.removeAttribute(t);return}le(e,t,void 0)!==r&&e.setAttribute(t,`${r}`)}function y(e,t){return e.hasAttribute(t)}function b(e,t,n){if(n==null){e.hasAttribute(t)&&e.removeAttribute(t);return}y(e,t)!=n&&e.toggleAttribute(t,n)}function x(e,t,n=null){return e.getAttribute(t)??n}function S(e,t,n){if(n==null){e.hasAttribute(t)&&e.removeAttribute(t);return}let r=`${n}`;x(e,t,void 0)!==r&&e.setAttribute(t,r)}var de=class{addEventListener(){}removeEventListener(){}dispatchEvent(){return!0}},fe=class extends de{},pe=class extends fe{constructor(){super(...arguments),this.role=null}},me=class{observe(){}unobserve(){}disconnect(){}},he={createElement:function(){return new ge.HTMLElement},createElementNS:function(){return new ge.HTMLElement},addEventListener(){},removeEventListener(){},dispatchEvent(e){return!1}},ge={ResizeObserver:me,document:he,Node:fe,Element:pe,HTMLElement:class extends pe{constructor(){super(...arguments),this.innerHTML=``}get content(){return new ge.DocumentFragment}},DocumentFragment:class extends de{},customElements:{get:function(){},define:function(){},whenDefined:function(){}},localStorage:{getItem(e){return null},setItem(e,t){},removeItem(e){}},CustomEvent:function(){},getComputedStyle:function(){},navigator:{languages:[],get userAgent(){return``}},matchMedia(e){return{matches:!1,media:e}},DOMParser:class{parseFromString(e,t){return{body:{textContent:e}}}}},_e=`global`in globalThis&&(globalThis==null?void 0:globalThis.global)===globalThis||typeof window>`u`||window.customElements===void 0,ve=Object.keys(ge).every(e=>e in globalThis),C=_e&&!ve?ge:globalThis,w=_e&&!ve?he:globalThis.document,ye={PLACEMENT:`placement`,BOUNDS:`bounds`};function be(e){return`
    <style>
      :host {
        --_tooltip-background-color: var(--media-tooltip-background-color, var(--media-secondary-color, rgba(20, 20, 30, .7)));
        --_tooltip-background: var(--media-tooltip-background, var(--_tooltip-background-color));
        --_tooltip-arrow-half-width: calc(var(--media-tooltip-arrow-width, 12px) / 2);
        --_tooltip-arrow-height: var(--media-tooltip-arrow-height, 5px);
        --_tooltip-arrow-background: var(--media-tooltip-arrow-color, var(--_tooltip-background-color));
        position: relative;
        pointer-events: none;
        display: var(--media-tooltip-display, inline-flex);
        justify-content: center;
        align-items: center;
        box-sizing: border-box;
        z-index: var(--media-tooltip-z-index, 1);
        background: var(--_tooltip-background);
        color: var(--media-text-color, var(--media-primary-color, rgb(238 238 238)));
        font: var(--media-font,
          var(--media-font-weight, 400)
          var(--media-font-size, 13px) /
          var(--media-text-content-height, var(--media-control-height, 18px))
          var(--media-font-family, helvetica neue, segoe ui, roboto, arial, sans-serif));
        padding: var(--media-tooltip-padding, .35em .7em);
        border: var(--media-tooltip-border, none);
        border-radius: var(--media-tooltip-border-radius, 5px);
        filter: var(--media-tooltip-filter, drop-shadow(0 0 4px rgba(0, 0, 0, .2)));
        white-space: var(--media-tooltip-white-space, nowrap);
      }

      :host([hidden]) {
        display: none;
      }

      img, svg {
        display: inline-block;
      }

      #arrow {
        position: absolute;
        width: 0px;
        height: 0px;
        border-style: solid;
        display: var(--media-tooltip-arrow-display, block);
      }

      :host(:not([placement])),
      :host([placement="top"]) {
        position: absolute;
        bottom: calc(100% + var(--media-tooltip-distance, 12px));
        left: 50%;
        transform: translate(calc(-50% - var(--media-tooltip-offset-x, 0px)), 0);
      }
      :host(:not([placement])) #arrow,
      :host([placement="top"]) #arrow {
        top: 100%;
        left: 50%;
        border-width: var(--_tooltip-arrow-height) var(--_tooltip-arrow-half-width) 0 var(--_tooltip-arrow-half-width);
        border-color: var(--_tooltip-arrow-background) transparent transparent transparent;
        transform: translate(calc(-50% + var(--media-tooltip-offset-x, 0px)), 0);
      }

      :host([placement="right"]) {
        position: absolute;
        left: calc(100% + var(--media-tooltip-distance, 12px));
        top: 50%;
        transform: translate(0, -50%);
      }
      :host([placement="right"]) #arrow {
        top: 50%;
        right: 100%;
        border-width: var(--_tooltip-arrow-half-width) var(--_tooltip-arrow-height) var(--_tooltip-arrow-half-width) 0;
        border-color: transparent var(--_tooltip-arrow-background) transparent transparent;
        transform: translate(0, -50%);
      }

      :host([placement="bottom"]) {
        position: absolute;
        top: calc(100% + var(--media-tooltip-distance, 12px));
        left: 50%;
        transform: translate(calc(-50% - var(--media-tooltip-offset-x, 0px)), 0);
      }
      :host([placement="bottom"]) #arrow {
        bottom: 100%;
        left: 50%;
        border-width: 0 var(--_tooltip-arrow-half-width) var(--_tooltip-arrow-height) var(--_tooltip-arrow-half-width);
        border-color: transparent transparent var(--_tooltip-arrow-background) transparent;
        transform: translate(calc(-50% + var(--media-tooltip-offset-x, 0px)), 0);
      }

      :host([placement="left"]) {
        position: absolute;
        right: calc(100% + var(--media-tooltip-distance, 12px));
        top: 50%;
        transform: translate(0, -50%);
      }
      :host([placement="left"]) #arrow {
        top: 50%;
        left: 100%;
        border-width: var(--_tooltip-arrow-half-width) 0 var(--_tooltip-arrow-half-width) var(--_tooltip-arrow-height);
        border-color: transparent transparent transparent var(--_tooltip-arrow-background);
        transform: translate(0, -50%);
      }
      
      :host([placement="none"]) #arrow {
        display: none;
      }
    </style>
    <slot></slot>
    <div id="arrow"></div>
  `}var xe=class extends C.HTMLElement{constructor(){if(super(),this.updateXOffset=()=>{if(!ae(this,{checkOpacity:!1,checkVisibilityCSS:!1}))return;let e=this.placement;if(e===`left`||e===`right`){this.style.removeProperty(`--media-tooltip-offset-x`);return}let t=getComputedStyle(this),n=_(this,`#`+this.bounds)??ee(this);if(!n)return;let{x:r,width:i}=n.getBoundingClientRect(),{x:a,width:o}=this.getBoundingClientRect(),s=a+o,c=r+i,l=t.getPropertyValue(`--media-tooltip-offset-x`),u=l?parseFloat(l.replace(`px`,``)):0,d=t.getPropertyValue(`--media-tooltip-container-margin`),f=d?parseFloat(d.replace(`px`,``)):0,p=a-r+u-f,m=s-c+u+f;if(p<0){this.style.setProperty(`--media-tooltip-offset-x`,`${p}px`);return}if(m>0){this.style.setProperty(`--media-tooltip-offset-x`,`${m}px`);return}this.style.removeProperty(`--media-tooltip-offset-x`)},!this.shadowRoot){this.attachShadow(this.constructor.shadowRootOptions);let e=g(this.attributes);this.shadowRoot.innerHTML=this.constructor.getTemplateHTML(e)}if(this.arrowEl=this.shadowRoot.querySelector(`#arrow`),Object.prototype.hasOwnProperty.call(this,`placement`)){let e=this.placement;delete this.placement,this.placement=e}}static get observedAttributes(){return[ye.PLACEMENT,ye.BOUNDS]}get placement(){return x(this,ye.PLACEMENT)}set placement(e){S(this,ye.PLACEMENT,e)}get bounds(){return x(this,ye.BOUNDS)}set bounds(e){S(this,ye.BOUNDS,e)}};xe.shadowRootOptions={mode:`open`},xe.getTemplateHTML=be,C.customElements.get(`media-tooltip`)||C.customElements.define(`media-tooltip`,xe);var Se=xe,Ce=(e,t,n)=>{if(!t.has(e))throw TypeError(`Cannot `+n)},T=(e,t,n)=>(Ce(e,t,`read from private field`),n?n.call(e):t.get(e)),we=(e,t,n)=>{if(t.has(e))throw TypeError(`Cannot add the same private member more than once`);t instanceof WeakSet?t.add(e):t.set(e,n)},Te=(e,t,n,r)=>(Ce(e,t,`write to private field`),r?r.call(e,n):t.set(e,n),n),Ee=(e,t,n)=>(Ce(e,t,`access private method`),n),E,De,D,O,Oe,ke,Ae,k={TOOLTIP_PLACEMENT:`tooltipplacement`,DISABLED:`disabled`,NO_TOOLTIP:`notooltip`};function je(e,t={}){return`
    <style>
      :host {
        position: relative;
        font: var(--media-font,
          var(--media-font-weight, bold)
          var(--media-font-size, 14px) /
          var(--media-text-content-height, var(--media-control-height, 24px))
          var(--media-font-family, helvetica neue, segoe ui, roboto, arial, sans-serif));
        color: var(--media-text-color, var(--media-primary-color, rgb(238 238 238)));
        background: var(--media-control-background, var(--media-secondary-color, rgb(20 20 30 / .7)));
        padding: var(--media-button-padding, var(--media-control-padding, 10px));
        justify-content: var(--media-button-justify-content, center);
        display: inline-flex;
        align-items: center;
        vertical-align: middle;
        box-sizing: border-box;
        transition: background .15s linear;
        pointer-events: auto;
        cursor: var(--media-cursor, pointer);
        -webkit-tap-highlight-color: transparent;
      }

      
      :host(:focus-visible) {
        box-shadow: var(--media-focus-box-shadow, inset 0 0 0 2px rgb(27 127 204 / .9));
        outline: 0;
      }
      
      :host(:where(:focus)) {
        box-shadow: none;
        outline: 0;
      }

      :host(:hover) {
        background: var(--media-control-hover-background, rgba(50 50 70 / .7));
      }

      slot[name="icon"] {
        display: inline-flex;
        align-items: center;
      }

      svg, img, ::slotted(svg), ::slotted(img) {
        width: var(--media-button-icon-width);
        height: var(--media-button-icon-height, var(--media-control-height, 24px));
        transform: var(--media-button-icon-transform);
        transition: var(--media-button-icon-transition);
        fill: var(--media-icon-color, var(--media-primary-color, rgb(238 238 238)));
        vertical-align: middle;
        max-width: 100%;
        max-height: 100%;
        min-width: 100%;
      }

      media-tooltip {
        
        max-width: 0;
        overflow-x: clip;
        opacity: 0;
        transition: opacity .3s, max-width 0s 9s;
      }

      :host(:hover) media-tooltip,
      :host(:focus-visible) media-tooltip {
        max-width: 100vw;
        opacity: 1;
        transition: opacity .3s;
      }

      :host([notooltip]) slot[name="tooltip"] {
        display: none;
      }
    </style>

    ${this.getSlotTemplateHTML(e,t)}

    <slot name="tooltip">
      <media-tooltip part="tooltip" aria-hidden="true">
        <template shadowrootmode="${Se.shadowRootOptions.mode}">
          ${Se.getTemplateHTML({})}
        </template>
        <slot name="tooltip-content">
          ${this.getTooltipContentHTML(e)}
        </slot>
      </media-tooltip>
    </slot>
  `}function Me(e,t){return`
    <slot></slot>
  `}function Ne(){return``}var A=class extends C.HTMLElement{constructor(){if(super(),we(this,ke),we(this,E,void 0),this.preventClick=!1,this.tooltipEl=null,we(this,De,e=>{this.preventClick||this.handleClick(e),setTimeout(T(this,D),0)}),we(this,D,()=>{var e,t;(t=(e=this.tooltipEl)?.updateXOffset)==null||t.call(e)}),we(this,O,e=>{let{key:t}=e;if(!this.keysUsed.includes(t)){this.removeEventListener(`keyup`,T(this,O));return}this.preventClick||this.handleClick(e)}),we(this,Oe,e=>{let{metaKey:t,altKey:n,key:r}=e;if(t||n||!this.keysUsed.includes(r)){this.removeEventListener(`keyup`,T(this,O));return}this.addEventListener(`keyup`,T(this,O),{once:!0})}),!this.shadowRoot){this.attachShadow(this.constructor.shadowRootOptions);let e=g(this.attributes),t=this.constructor.getTemplateHTML(e);this.shadowRoot.setHTMLUnsafe?this.shadowRoot.setHTMLUnsafe(t):this.shadowRoot.innerHTML=t}this.tooltipEl=this.shadowRoot.querySelector(`media-tooltip`)}static get observedAttributes(){return[`disabled`,k.TOOLTIP_PLACEMENT,i.MEDIA_CONTROLLER,s.MEDIA_LANG]}enable(){this.addEventListener(`click`,T(this,De)),this.addEventListener(`keydown`,T(this,Oe)),this.tabIndex=0}disable(){this.removeEventListener(`click`,T(this,De)),this.removeEventListener(`keydown`,T(this,Oe)),this.removeEventListener(`keyup`,T(this,O)),this.tabIndex=-1}attributeChangedCallback(e,t,n){var r,a,o,c;e===i.MEDIA_CONTROLLER?(t&&((a=(r=T(this,E))?.unassociateElement)==null||a.call(r,this),Te(this,E,null)),n&&this.isConnected&&(Te(this,E,this.getRootNode()?.getElementById(n)),(c=(o=T(this,E))?.associateElement)==null||c.call(o,this))):e===`disabled`&&n!==t?n==null?this.enable():this.disable():e===k.TOOLTIP_PLACEMENT&&this.tooltipEl&&n!==t?this.tooltipEl.placement=n:e===s.MEDIA_LANG&&(this.shadowRoot.querySelector(`slot[name="tooltip-content"]`).innerHTML=this.constructor.getTooltipContentHTML()),T(this,D).call(this)}connectedCallback(){var e,t;let{style:n}=v(this.shadowRoot,`:host`);n.setProperty(`display`,`var(--media-control-display, var(--${this.localName}-display, inline-flex))`),this.hasAttribute(`disabled`)?this.disable():this.enable(),this.setAttribute(`role`,`button`);let r=this.getAttribute(i.MEDIA_CONTROLLER);r&&(Te(this,E,this.getRootNode()?.getElementById(r)),(t=(e=T(this,E))?.associateElement)==null||t.call(e,this)),C.customElements.whenDefined(`media-tooltip`).then(()=>Ee(this,ke,Ae).call(this))}disconnectedCallback(){var e,t;this.disable(),(t=(e=T(this,E))?.unassociateElement)==null||t.call(e,this),Te(this,E,null),this.removeEventListener(`mouseenter`,T(this,D)),this.removeEventListener(`focus`,T(this,D)),this.removeEventListener(`click`,T(this,De))}get keysUsed(){return[`Enter`,` `]}get tooltipPlacement(){return x(this,k.TOOLTIP_PLACEMENT)}set tooltipPlacement(e){S(this,k.TOOLTIP_PLACEMENT,e)}get mediaController(){return x(this,i.MEDIA_CONTROLLER)}set mediaController(e){S(this,i.MEDIA_CONTROLLER,e)}get disabled(){return y(this,k.DISABLED)}set disabled(e){b(this,k.DISABLED,e)}get noTooltip(){return y(this,k.NO_TOOLTIP)}set noTooltip(e){b(this,k.NO_TOOLTIP,e)}handleClick(e){}};E=new WeakMap,De=new WeakMap,D=new WeakMap,O=new WeakMap,Oe=new WeakMap,ke=new WeakSet,Ae=function(){this.addEventListener(`mouseenter`,T(this,D)),this.addEventListener(`focus`,T(this,D)),this.addEventListener(`click`,T(this,De));let e=this.tooltipPlacement;e&&this.tooltipEl&&(this.tooltipEl.placement=e)},A.shadowRootOptions={mode:`open`},A.getTemplateHTML=je,A.getSlotTemplateHTML=Me,A.getTooltipContentHTML=Ne,C.customElements.get(`media-chrome-button`)||C.customElements.define(`media-chrome-button`,A);var Pe=`<svg aria-hidden="true" viewBox="0 0 24 24">
  <path d="M16.5 12A4.5 4.5 0 0 0 14 8v2.18l2.45 2.45a4.22 4.22 0 0 0 .05-.63Zm2.5 0a6.84 6.84 0 0 1-.54 2.64L20 16.15A8.8 8.8 0 0 0 21 12a9 9 0 0 0-7-8.77v2.06A7 7 0 0 1 19 12ZM4.27 3 3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25A6.92 6.92 0 0 1 14 18.7v2.06A9 9 0 0 0 17.69 19l2 2.05L21 19.73l-9-9L4.27 3ZM12 4 9.91 6.09 12 8.18V4Z"/>
</svg>`,Fe=`<svg aria-hidden="true" viewBox="0 0 24 24">
  <path d="M3 9v6h4l5 5V4L7 9H3Zm13.5 3A4.5 4.5 0 0 0 14 8v8a4.47 4.47 0 0 0 2.5-4Z"/>
</svg>`,Ie=`<svg aria-hidden="true" viewBox="0 0 24 24">
  <path d="M3 9v6h4l5 5V4L7 9H3Zm13.5 3A4.5 4.5 0 0 0 14 8v8a4.47 4.47 0 0 0 2.5-4ZM14 3.23v2.06a7 7 0 0 1 0 13.42v2.06a9 9 0 0 0 0-17.54Z"/>
</svg>`;function Le(e){return`
    <style>
      :host(:not([${s.MEDIA_VOLUME_LEVEL}])) slot[name=icon] slot:not([name=high]),
      :host([${s.MEDIA_VOLUME_LEVEL}=high]) slot[name=icon] slot:not([name=high]) {
        display: none !important;
      }

      :host([${s.MEDIA_VOLUME_LEVEL}=off]) slot[name=icon] slot:not([name=off]) {
        display: none !important;
      }

      :host([${s.MEDIA_VOLUME_LEVEL}=low]) slot[name=icon] slot:not([name=low]) {
        display: none !important;
      }

      :host([${s.MEDIA_VOLUME_LEVEL}=medium]) slot[name=icon] slot:not([name=medium]) {
        display: none !important;
      }

      :host(:not([${s.MEDIA_VOLUME_LEVEL}=off])) slot[name=tooltip-unmute],
      :host([${s.MEDIA_VOLUME_LEVEL}=off]) slot[name=tooltip-mute] {
        display: none;
      }
    </style>

    <slot name="icon">
      <slot name="off">${Pe}</slot>
      <slot name="low">${Fe}</slot>
      <slot name="medium">${Fe}</slot>
      <slot name="high">${Ie}</slot>
    </slot>
  `}function Re(){return`
    <slot name="tooltip-mute">${e(`Mute`)}</slot>
    <slot name="tooltip-unmute">${e(`Unmute`)}</slot>
  `}var ze=t=>{let n=t.mediaVolumeLevel===`off`?e(`unmute`):e(`mute`);t.setAttribute(`aria-label`,n)},Be=class extends A{static get observedAttributes(){return[...super.observedAttributes,s.MEDIA_VOLUME_LEVEL]}connectedCallback(){super.connectedCallback(),ze(this)}attributeChangedCallback(e,t,n){super.attributeChangedCallback(e,t,n),e===s.MEDIA_VOLUME_LEVEL&&ze(this)}get mediaVolumeLevel(){return x(this,s.MEDIA_VOLUME_LEVEL)}set mediaVolumeLevel(e){S(this,s.MEDIA_VOLUME_LEVEL,e)}handleClick(){let e=this.mediaVolumeLevel===`off`?r.MEDIA_UNMUTE_REQUEST:r.MEDIA_MUTE_REQUEST;this.dispatchEvent(new C.CustomEvent(e,{composed:!0,bubbles:!0}))}};Be.getSlotTemplateHTML=Le,Be.getTooltipContentHTML=Re,C.customElements.get(`media-mute-button`)||C.customElements.define(`media-mute-button`,Be);var Ve=new WeakMap,He=e=>{let t=Ve.get(e);return t||Ve.set(e,t=new Set),t},Ue=new C.ResizeObserver(e=>{for(let t of e)for(let e of He(t.target))e(t)});function We(e,t){He(e).add(t),Ue.observe(e)}function Ge(e,t){let n=He(e);n.delete(t),n.size||Ue.unobserve(e)}var Ke=(e,t,n)=>{if(!t.has(e))throw TypeError(`Cannot `+n)},j=(e,t,n)=>(Ke(e,t,`read from private field`),n?n.call(e):t.get(e)),qe=(e,t,n)=>{if(t.has(e))throw TypeError(`Cannot add the same private member more than once`);t instanceof WeakSet?t.add(e):t.set(e,n)},Je=(e,t,n,r)=>(Ke(e,t,`write to private field`),r?r.call(e,n):t.set(e,n),n),M;function Ye(e){return`
    <style>
      :host {
        display: var(--media-control-display, var(--media-gesture-receiver-display, inline-block));
        box-sizing: border-box;
      }
    </style>
  `}var Xe=class extends C.HTMLElement{constructor(){if(super(),qe(this,M,void 0),!this.shadowRoot){this.attachShadow(this.constructor.shadowRootOptions);let e=g(this.attributes);this.shadowRoot.innerHTML=this.constructor.getTemplateHTML(e)}}static get observedAttributes(){return[i.MEDIA_CONTROLLER,s.MEDIA_PAUSED]}attributeChangedCallback(e,t,n){var r,a,o,s;e===i.MEDIA_CONTROLLER&&(t&&((a=(r=j(this,M))?.unassociateElement)==null||a.call(r,this),Je(this,M,null)),n&&this.isConnected&&(Je(this,M,this.getRootNode()?.getElementById(n)),(s=(o=j(this,M))?.associateElement)==null||s.call(o,this)))}connectedCallback(){var e,t;this.tabIndex=-1,this.setAttribute(`aria-hidden`,`true`),Je(this,M,Ze(this)),this.getAttribute(i.MEDIA_CONTROLLER)&&((t=(e=j(this,M))?.associateElement)==null||t.call(e,this)),j(this,M)&&(j(this,M).addEventListener(`pointerdown`,this),j(this,M).addEventListener(`click`,this),j(this,M).hasAttribute(`tabindex`)||(j(this,M).tabIndex=0))}disconnectedCallback(){var e,t,n,r;this.getAttribute(i.MEDIA_CONTROLLER)&&((t=(e=j(this,M))?.unassociateElement)==null||t.call(e,this)),(n=j(this,M))==null||n.removeEventListener(`pointerdown`,this),(r=j(this,M))==null||r.removeEventListener(`click`,this),Je(this,M,null)}handleEvent(e){let t=e.composedPath()?.[0];if([`video`,`media-controller`].includes(t?.localName)){if(e.type===`pointerdown`)this._pointerType=e.pointerType;else if(e.type===`click`){let{clientX:t,clientY:n}=e,{left:r,top:i,width:a,height:o}=this.getBoundingClientRect(),s=t-r,c=n-i;if(s<0||c<0||s>a||c>o||a===0&&o===0)return;let l=this._pointerType||`mouse`;if(this._pointerType=void 0,l===f.TOUCH){this.handleTap(e);return}if(l===f.MOUSE||l===f.PEN){this.handleMouseClick(e);return}}}}get mediaPaused(){return y(this,s.MEDIA_PAUSED)}set mediaPaused(e){b(this,s.MEDIA_PAUSED,e)}handleTap(e){}handleMouseClick(e){let t=this.mediaPaused?r.MEDIA_PLAY_REQUEST:r.MEDIA_PAUSE_REQUEST;this.dispatchEvent(new C.CustomEvent(t,{composed:!0,bubbles:!0}))}};M=new WeakMap,Xe.shadowRootOptions={mode:`open`},Xe.getTemplateHTML=Ye;function Ze(e){let t=e.getAttribute(i.MEDIA_CONTROLLER);return t?e.getRootNode()?.getElementById(t):_(e,`media-controller`)}C.customElements.get(`media-gesture-receiver`)||C.customElements.define(`media-gesture-receiver`,Xe);var Qe=Xe,$e=(e,t,n)=>{if(!t.has(e))throw TypeError(`Cannot `+n)},N=(e,t,n)=>($e(e,t,`read from private field`),n?n.call(e):t.get(e)),P=(e,t,n)=>{if(t.has(e))throw TypeError(`Cannot add the same private member more than once`);t instanceof WeakSet?t.add(e):t.set(e,n)},F=(e,t,n,r)=>($e(e,t,`write to private field`),r?r.call(e,n):t.set(e,n),n),I=(e,t,n)=>($e(e,t,`access private method`),n),et,tt,nt,rt,it,at,L,ot,st,ct,lt,ut,dt,ft,pt,mt,ht,gt,R,_t,z={AUDIO:`audio`,AUTOHIDE:`autohide`,BREAKPOINTS:`breakpoints`,GESTURES_DISABLED:`gesturesdisabled`,KEYBOARD_CONTROL:`keyboardcontrol`,NO_AUTOHIDE:`noautohide`,USER_INACTIVE:`userinactive`,AUTOHIDE_OVER_CONTROLS:`autohideovercontrols`};function vt(e){return`
    <style>
      
      :host([${s.MEDIA_IS_FULLSCREEN}]) ::slotted([slot=media]) {
        outline: none;
      }

      :host {
        box-sizing: border-box;
        position: relative;
        display: inline-block;
        line-height: 0;
        background-color: var(--media-background-color, #000);
        overflow: hidden;
      }

      :host(:not([${z.AUDIO}])) [part~=layer]:not([part~=media-layer]) {
        position: absolute;
        top: 0;
        left: 0;
        bottom: 0;
        right: 0;
        display: flex;
        flex-flow: column nowrap;
        align-items: start;
        pointer-events: none;
        background: none;
      }

      slot[name=media] {
        display: var(--media-slot-display, contents);
      }

      
      :host([${z.AUDIO}]) slot[name=media] {
        display: var(--media-slot-display, none);
      }

      
      :host([${z.AUDIO}]) [part~=layer][part~=gesture-layer] {
        height: 0;
        display: block;
      }

      
      :host(:not([${z.AUDIO}])[${z.GESTURES_DISABLED}]) ::slotted([slot=gestures-chrome]),
          :host(:not([${z.AUDIO}])[${z.GESTURES_DISABLED}]) media-gesture-receiver[slot=gestures-chrome] {
        display: none;
      }

      
      ::slotted(:not([slot=media]):not([slot=poster]):not(media-loading-indicator):not([role=dialog]):not([hidden])) {
        pointer-events: auto;
      }

      :host(:not([${z.AUDIO}])) *[part~=layer][part~=centered-layer] {
        align-items: center;
        justify-content: center;
      }

      :host(:not([${z.AUDIO}])) ::slotted(media-gesture-receiver[slot=gestures-chrome]),
      :host(:not([${z.AUDIO}])) media-gesture-receiver[slot=gestures-chrome] {
        align-self: stretch;
        flex-grow: 1;
      }

      slot[name=middle-chrome] {
        display: inline;
        flex-grow: 1;
        pointer-events: none;
        background: none;
      }

      
      ::slotted([slot=media]),
      ::slotted([slot=poster]) {
        width: 100%;
        height: 100%;
      }

      
      :host(:not([${z.AUDIO}])) .spacer {
        flex-grow: 1;
      }

      
      :host(:-webkit-full-screen) {
        
        width: 100% !important;
        height: 100% !important;
      }

      
      ::slotted(:not([slot=media]):not([slot=poster]):not([${z.NO_AUTOHIDE}]):not([hidden]):not([role=dialog])) {
        opacity: 1;
        transition: var(--media-control-transition-in, opacity 0.25s);
      }

      
      :host([${z.USER_INACTIVE}]:not([${s.MEDIA_PAUSED}]):not([${s.MEDIA_IS_AIRPLAYING}]):not([${s.MEDIA_IS_CASTING}]):not([${z.AUDIO}])) ::slotted(:not([slot=media]):not([slot=poster]):not([${z.NO_AUTOHIDE}]):not([role=dialog])) {
        opacity: 0;
        transition: var(--media-control-transition-out, opacity 1s);
      }

      :host([${z.USER_INACTIVE}]:not([${z.NO_AUTOHIDE}]):not([${s.MEDIA_PAUSED}]):not([${s.MEDIA_IS_CASTING}]):not([${z.AUDIO}])) ::slotted([slot=media]) {
        cursor: none;
      }

      :host([${z.USER_INACTIVE}][${z.AUTOHIDE_OVER_CONTROLS}]:not([${z.NO_AUTOHIDE}]):not([${s.MEDIA_PAUSED}]):not([${s.MEDIA_IS_CASTING}]):not([${z.AUDIO}])) * {
        --media-cursor: none;
        cursor: none;
      }


      ::slotted(media-control-bar)  {
        align-self: stretch;
      }

      
      :host(:not([${z.AUDIO}])[${s.MEDIA_HAS_PLAYED}]) slot[name=poster] {
        display: none;
      }

      ::slotted([role=dialog]) {
        width: 100%;
        height: 100%;
        align-self: center;
      }

      ::slotted([role=menu]) {
        align-self: end;
      }
    </style>

    <slot name="media" part="layer media-layer"></slot>
    <slot name="poster" part="layer poster-layer"></slot>
    <slot name="gestures-chrome" part="layer gesture-layer">
      <media-gesture-receiver slot="gestures-chrome">
        <template shadowrootmode="${Qe.shadowRootOptions.mode}">
          ${Qe.getTemplateHTML({})}
        </template>
      </media-gesture-receiver>
    </slot>
    <span part="layer vertical-layer">
      <slot name="top-chrome" part="top chrome"></slot>
      <slot name="middle-chrome" part="middle chrome"></slot>
      <slot name="centered-chrome" part="layer centered-layer center centered chrome"></slot>
      
      <slot part="bottom chrome"></slot>
    </span>
    <slot name="dialog" part="layer dialog-layer"></slot>
  `}var yt=Object.values(s),bt=`sm:384 md:576 lg:768 xl:960`;function xt(e){St(e.target,e.contentRect.width)}function St(e,t){if(!e.isConnected)return;let n=Ct(e.getAttribute(z.BREAKPOINTS)??bt),r=wt(n,t),i=!1;if(Object.keys(n).forEach(t=>{if(r.includes(t)){e.hasAttribute(`breakpoint${t}`)||(e.setAttribute(`breakpoint${t}`,``),i=!0);return}e.hasAttribute(`breakpoint${t}`)&&(e.removeAttribute(`breakpoint${t}`),i=!0)}),i){let t=new CustomEvent(c.BREAKPOINTS_CHANGE,{detail:r});e.dispatchEvent(t)}e.breakpointsComputed||(e.breakpointsComputed=!0,e.dispatchEvent(new CustomEvent(c.BREAKPOINTS_COMPUTED,{bubbles:!0,composed:!0})))}function Ct(e){let t=e.split(/\s+/);return Object.fromEntries(t.map(e=>e.split(`:`)))}function wt(e,t){return Object.keys(e).filter(n=>t>=parseInt(e[n]))}var Tt=class extends C.HTMLElement{constructor(){if(super(),P(this,st),P(this,lt),P(this,dt),P(this,pt),P(this,ht),P(this,et,void 0),P(this,tt,0),P(this,nt,null),P(this,rt,null),P(this,it,void 0),this.breakpointsComputed=!1,P(this,at,e=>{let t=this.media;for(let n of e){if(n.type!==`childList`)continue;let e=n.removedNodes;for(let r of e){if(r.slot!=`media`||n.target!=this)continue;let e=n.previousSibling&&n.previousSibling.previousElementSibling;if(!e||!t)this.mediaUnsetCallback(r);else{let t=e.slot!==`media`;for(;(e=e.previousSibling)!==null;)e.slot==`media`&&(t=!1);t&&this.mediaUnsetCallback(r)}}if(t)for(let e of n.addedNodes)e===t&&this.handleMediaUpdated(t)}}),P(this,L,!1),P(this,ot,e=>{N(this,L)||(setTimeout(()=>{xt(e),F(this,L,!1)},0),F(this,L,!0))}),P(this,R,void 0),P(this,_t,()=>{if(!N(this,R).assignedElements({flatten:!0}).length){N(this,nt)&&this.mediaUnsetCallback(N(this,nt));return}this.handleMediaUpdated(this.media)}),!this.shadowRoot){this.attachShadow(this.constructor.shadowRootOptions);let e=g(this.attributes),t=this.constructor.getTemplateHTML(e);this.shadowRoot.setHTMLUnsafe?this.shadowRoot.setHTMLUnsafe(t):this.shadowRoot.innerHTML=t}F(this,et,new MutationObserver(N(this,at)))}static get observedAttributes(){return[z.AUTOHIDE,z.GESTURES_DISABLED].concat(yt).filter(e=>![s.MEDIA_RENDITION_LIST,s.MEDIA_AUDIO_TRACK_LIST,s.MEDIA_CHAPTERS_CUES,s.MEDIA_WIDTH,s.MEDIA_HEIGHT,s.MEDIA_ERROR,s.MEDIA_ERROR_MESSAGE].includes(e))}attributeChangedCallback(e,t,n){e.toLowerCase()==z.AUTOHIDE&&(this.autohide=n)}get media(){let e=this.querySelector(`:scope > [slot=media]`);return e?.nodeName==`SLOT`&&(e=e.assignedElements({flatten:!0})[0]),e}async handleMediaUpdated(e){e&&(F(this,nt,e),e.localName.includes(`-`)&&await C.customElements.whenDefined(e.localName),this.mediaSetCallback(e))}connectedCallback(){var t;N(this,et).observe(this,{childList:!0,subtree:!0}),We(this,N(this,ot));let n=this.getAttribute(z.AUDIO)==null?e(`video player`):e(`audio player`);this.setAttribute(`role`,`region`),this.setAttribute(`aria-label`,n),this.handleMediaUpdated(this.media),this.setAttribute(z.USER_INACTIVE,``),St(this,this.getBoundingClientRect().width);let r=this.querySelector(`:scope > slot[slot=media]`);r&&(F(this,R,r),N(this,R).addEventListener(`slotchange`,N(this,_t))),this.addEventListener(`pointerdown`,this),this.addEventListener(`pointermove`,this),this.addEventListener(`pointerup`,this),this.addEventListener(`mouseleave`,this),this.addEventListener(`keyup`,this),(t=C.window)==null||t.addEventListener(`mouseup`,this)}disconnectedCallback(){var e;Ge(this,N(this,ot)),clearTimeout(N(this,rt)),N(this,et).disconnect(),this.media&&this.mediaUnsetCallback(this.media),(e=C.window)==null||e.removeEventListener(`mouseup`,this),this.removeEventListener(`pointerdown`,this),this.removeEventListener(`pointermove`,this),this.removeEventListener(`pointerup`,this),this.removeEventListener(`mouseleave`,this),this.removeEventListener(`keyup`,this),N(this,R)&&(N(this,R).removeEventListener(`slotchange`,N(this,_t)),F(this,R,null)),F(this,L,!1)}mediaSetCallback(e){}mediaUnsetCallback(e){F(this,nt,null)}handleEvent(e){switch(e.type){case`pointerdown`:F(this,tt,e.timeStamp);break;case`pointermove`:I(this,st,ct).call(this,e);break;case`pointerup`:I(this,lt,ut).call(this,e);break;case`mouseleave`:I(this,dt,ft).call(this);break;case`mouseup`:this.removeAttribute(z.KEYBOARD_CONTROL);break;case`keyup`:I(this,ht,gt).call(this),this.setAttribute(z.KEYBOARD_CONTROL,``)}}set autohide(e){let t=Number(e);F(this,it,isNaN(t)?0:t)}get autohide(){return(N(this,it)===void 0?2:N(this,it)).toString()}get breakpoints(){return x(this,z.BREAKPOINTS)}set breakpoints(e){S(this,z.BREAKPOINTS,e)}get audio(){return y(this,z.AUDIO)}set audio(e){b(this,z.AUDIO,e)}get gesturesDisabled(){return y(this,z.GESTURES_DISABLED)}set gesturesDisabled(e){b(this,z.GESTURES_DISABLED,e)}get keyboardControl(){return y(this,z.KEYBOARD_CONTROL)}set keyboardControl(e){b(this,z.KEYBOARD_CONTROL,e)}get noAutohide(){return y(this,z.NO_AUTOHIDE)}set noAutohide(e){b(this,z.NO_AUTOHIDE,e)}get autohideOverControls(){return y(this,z.AUTOHIDE_OVER_CONTROLS)}set autohideOverControls(e){b(this,z.AUTOHIDE_OVER_CONTROLS,e)}get userInteractive(){return y(this,z.USER_INACTIVE)}set userInteractive(e){b(this,z.USER_INACTIVE,e)}};et=new WeakMap,tt=new WeakMap,nt=new WeakMap,rt=new WeakMap,it=new WeakMap,at=new WeakMap,L=new WeakMap,ot=new WeakMap,st=new WeakSet,ct=function(e){if(e.pointerType!==`mouse`&&e.timeStamp-N(this,tt)<250)return;I(this,pt,mt).call(this),clearTimeout(N(this,rt));let t=this.hasAttribute(z.AUTOHIDE_OVER_CONTROLS);([this,this.media].includes(e.target)||t)&&I(this,ht,gt).call(this)},lt=new WeakSet,ut=function(e){if(e.pointerType===`touch`){let t=!this.hasAttribute(z.USER_INACTIVE);[this,this.media].includes(e.target)&&t?I(this,dt,ft).call(this):I(this,ht,gt).call(this)}else e.composedPath().some(e=>[`media-play-button`,`media-fullscreen-button`].includes(e?.localName))&&I(this,ht,gt).call(this)},dt=new WeakSet,ft=function(){if(N(this,it)<0||this.hasAttribute(z.USER_INACTIVE))return;this.setAttribute(z.USER_INACTIVE,``);let e=new C.CustomEvent(c.USER_INACTIVE_CHANGE,{composed:!0,bubbles:!0,detail:!0});this.dispatchEvent(e)},pt=new WeakSet,mt=function(){if(!this.hasAttribute(z.USER_INACTIVE))return;this.removeAttribute(z.USER_INACTIVE);let e=new C.CustomEvent(c.USER_INACTIVE_CHANGE,{composed:!0,bubbles:!0,detail:!1});this.dispatchEvent(e)},ht=new WeakSet,gt=function(){I(this,pt,mt).call(this),clearTimeout(N(this,rt));let e=parseInt(this.autohide);e<0||F(this,rt,setTimeout(()=>{I(this,dt,ft).call(this)},e*1e3))},R=new WeakMap,_t=new WeakMap,Tt.shadowRootOptions={mode:`open`},Tt.getTemplateHTML=vt,C.customElements.get(`media-container`)||C.customElements.define(`media-container`,Tt);var Et=(e,t,n)=>{if(!t.has(e))throw TypeError(`Cannot `+n)},B=(e,t,n)=>(Et(e,t,`read from private field`),n?n.call(e):t.get(e)),Dt=(e,t,n)=>{if(t.has(e))throw TypeError(`Cannot add the same private member more than once`);t instanceof WeakSet?t.add(e):t.set(e,n)},Ot=(e,t,n,r)=>(Et(e,t,`write to private field`),r?r.call(e,n):t.set(e,n),n),kt,At,jt,V,H,U,Mt=class{constructor(e,t,{defaultValue:n}={defaultValue:void 0}){Dt(this,H),Dt(this,kt,void 0),Dt(this,At,void 0),Dt(this,jt,void 0),Dt(this,V,new Set),Ot(this,kt,e),Ot(this,At,t),Ot(this,jt,new Set(n))}[Symbol.iterator](){return B(this,H,U).values()}get length(){return B(this,H,U).size}get value(){return[...B(this,H,U)].join(` `)??``}set value(e){e!==this.value&&(Ot(this,V,new Set),this.add(...e?.split(` `)??[]))}toString(){return this.value}item(e){return[...B(this,H,U)][e]}values(){return B(this,H,U).values()}forEach(e,t){B(this,H,U).forEach(e,t)}add(...e){var t;e.forEach(e=>B(this,V).add(e)),!(this.value===``&&!B(this,kt)?.hasAttribute(`${B(this,At)}`))&&((t=B(this,kt))==null||t.setAttribute(`${B(this,At)}`,`${this.value}`))}remove(...e){var t;e.forEach(e=>B(this,V).delete(e)),(t=B(this,kt))==null||t.setAttribute(`${B(this,At)}`,`${this.value}`)}contains(e){return B(this,H,U).has(e)}toggle(e,t){return t===void 0?this.contains(e)?(this.remove(e),!1):(this.add(e),!0):t?(this.add(e),!0):(this.remove(e),!1)}replace(e,t){return this.remove(e),this.add(t),e===t}};kt=new WeakMap,At=new WeakMap,jt=new WeakMap,V=new WeakMap,H=new WeakSet,U=function(){return B(this,V).size?B(this,V):B(this,jt)};function Nt(e){return e?.map(Ft).join(` `)}function Pt(e){return e?.split(/\s+/).map(It)}function Ft(e){if(e){let{id:t,width:n,height:r}=e;return[t,n,r].filter(e=>e!=null).join(`:`)}}function It(e){if(e){let[t,n,r]=e.split(`:`);return{id:t,width:+n,height:+r}}}function Lt(e){return e?.map(Rt).join(` `)}function Rt(e){if(e){let{id:t,kind:n,language:r,label:i}=e;return[t,n,r,i].filter(e=>e!=null).join(`:`)}}function zt(e){return typeof e==`number`&&!Number.isNaN(e)&&Number.isFinite(e)}var Bt=e=>new Promise(t=>setTimeout(t,e)),Vt=(e=``)=>e.split(/\s+/),Ht=(e=``)=>{let[t,n,r]=e.split(`:`),i=r?decodeURIComponent(r):void 0;return{kind:t===`cc`?u.CAPTIONS:u.SUBTITLES,language:n,label:i}},Ut=(e=``,t={})=>Vt(e).map(e=>{let n=Ht(e);return{...t,...n}}),Wt=e=>e?Array.isArray(e)?e.map(e=>typeof e==`string`?Ht(e):e):typeof e==`string`?Ut(e):[e]:[],Gt=({kind:e,label:t,language:n}={kind:`subtitles`})=>t?`${e===`captions`?`cc`:`sb`}:${n}:${encodeURIComponent(t)}`:n,Kt=(e=[])=>Array.prototype.map.call(e,Gt).join(` `),qt=(e,t)=>n=>n[e]===t,Jt=e=>{let t=Object.entries(e).map(([e,t])=>qt(e,t));return e=>t.every(t=>t(e))},Yt=(e,t=[],n=[])=>{let r=Wt(n).map(Jt);Array.from(t).filter(e=>r.some(t=>t(e))).forEach(t=>{t.mode=e})},Xt=(e,t=()=>!0)=>{if(!e?.textTracks)return[];let n=typeof t==`function`?t:Jt(t);return Array.from(e.textTracks).filter(n)},Zt=e=>{let{media:t,fullscreenElement:n}=e;try{let e=n&&`requestFullscreen`in n?`requestFullscreen`:n&&`webkitRequestFullScreen`in n?`webkitRequestFullScreen`:void 0;if(e){let t=n[e]?.call(n);if(t instanceof Promise)return t.catch(()=>{})}else t?.webkitEnterFullscreen?t.webkitEnterFullscreen():t?.requestFullscreen&&t.requestFullscreen()}catch(e){console.error(e)}},Qt=`exitFullscreen`in w?`exitFullscreen`:`webkitExitFullscreen`in w?`webkitExitFullscreen`:`webkitCancelFullScreen`in w?`webkitCancelFullScreen`:void 0,$t=e=>{let{documentElement:t}=e;if(Qt){let e=(t?.[Qt])?.call(t);if(e instanceof Promise)return e.catch(()=>{})}},en=`fullscreenElement`in w?`fullscreenElement`:`webkitFullscreenElement`in w?`webkitFullscreenElement`:void 0,tn=e=>{let{documentElement:t,media:n}=e,r=t?.[en];return!r&&`webkitDisplayingFullscreen`in n&&`webkitPresentationMode`in n&&n.webkitDisplayingFullscreen&&n.webkitPresentationMode===h.FULLSCREEN?n:r},nn=e=>{let{media:t,documentElement:n,fullscreenElement:r=t}=e;if(!t||!n)return!1;let i=tn(e);if(!i)return!1;if(i===r||i===t)return!0;if(i.localName.includes(`-`)){let e=i.shadowRoot;if(!(en in e))return ne(i,r);for(;e?.[en];){if(e[en]===r)return!0;e=e[en]?.shadowRoot}}return!1},rn=`fullscreenEnabled`in w?`fullscreenEnabled`:`webkitFullscreenEnabled`in w?`webkitFullscreenEnabled`:void 0,an=e=>{let{documentElement:t,media:n}=e;return!!t?.[rn]||n&&`webkitSupportsFullscreen`in n},on,sn=()=>{var e;return on||(on=((e=w)?.createElement)?.call(e,`video`),on)},cn=async(e=sn())=>{if(!e)return!1;let t=e.volume;e.volume=t/2+.1;let n=new AbortController,r=await Promise.race([ln(e,n.signal),un(e,t)]);return n.abort(),r},ln=(e,t)=>new Promise(n=>{e.addEventListener(`volumechange`,()=>n(!0),{signal:t})}),un=async(e,t)=>{for(let n=0;n<10;n++){if(e.volume===t)return!1;await Bt(10)}return e.volume!==t},dn=/.*Version\/.*Safari\/.*/.test(C.navigator.userAgent),fn=(e=sn())=>C.matchMedia(`(display-mode: standalone)`).matches&&dn?!1:typeof e?.requestPictureInPicture==`function`,pn=(e=sn())=>an({documentElement:w,media:e}),mn=pn(),hn=fn(),gn=!!C.WebKitPlaybackTargetAvailabilityEvent,_n=!!C.chrome,vn=e=>Xt(e.media,e=>[u.SUBTITLES,u.CAPTIONS].includes(e.kind)).sort((e,t)=>e.kind>=t.kind?1:-1),yn=e=>Xt(e.media,e=>e.mode===d.SHOWING&&[u.SUBTITLES,u.CAPTIONS].includes(e.kind)),bn=(e,t)=>{let n=vn(e),r=yn(e),i=!!r.length;if(n.length){if(t===!1||i&&t!==!0)Yt(d.DISABLED,n,r);else if(t===!0||!i&&t!==!1){let t=n[0],{options:i}=e;if(!i?.noSubtitlesLangPref){let e=C.localStorage.getItem(`media-chrome-pref-subtitles-lang`),r=e?[e,...C.navigator.languages]:C.navigator.languages,i=n.filter(e=>r.some(t=>e.language.toLowerCase().startsWith(t.split(`-`)[0]))).sort((e,t)=>r.findIndex(t=>e.language.toLowerCase().startsWith(t.split(`-`)[0]))-r.findIndex(e=>t.language.toLowerCase().startsWith(e.split(`-`)[0])));i[0]&&(t=i[0])}let{language:a,label:o,kind:s}=t;Yt(d.DISABLED,n,r),Yt(d.SHOWING,n,[{language:a,label:o,kind:s}])}}},xn=(e,t)=>e===t?!0:e==null||t==null||typeof e!=typeof t?!1:typeof e==`number`&&Number.isNaN(e)&&Number.isNaN(t)?!0:typeof e==`object`?Array.isArray(e)?Sn(e,t):Object.entries(e).every(([e,n])=>e in t&&xn(n,t[e])):!1,Sn=(e,t)=>{let n=Array.isArray(e),r=Array.isArray(t);return n===r?n||r?e.length===t.length&&e.every((e,n)=>xn(e,t[n])):!0:!1},Cn=Object.values(m),wn,Tn=cn().then(e=>(wn=e,wn)),En=async(...e)=>{await Promise.all(e.filter(e=>e).map(async e=>{if(!(`localName`in e&&e instanceof C.HTMLElement))return;let t=e.localName;if(!t.includes(`-`))return;let n=C.customElements.get(t);n&&e instanceof n||(await C.customElements.whenDefined(t),C.customElements.upgrade(e))}))},Dn=new C.DOMParser,On=e=>e&&(Dn.parseFromString(e,`text/html`).body.textContent||e),kn={mediaError:{get(e,t){let{media:n}=e;if(t?.type!==`playing`)return n?.error},mediaEvents:[`emptied`,`error`,`playing`]},mediaErrorCode:{get(e,t){let{media:n}=e;if(t?.type!==`playing`)return n?.error?.code},mediaEvents:[`emptied`,`error`,`playing`]},mediaErrorMessage:{get(e,t){let{media:n}=e;if(t?.type!==`playing`)return n?.error?.message??``},mediaEvents:[`emptied`,`error`,`playing`]},mediaWidth:{get(e){let{media:t}=e;return t?.videoWidth??0},mediaEvents:[`resize`]},mediaHeight:{get(e){let{media:t}=e;return t?.videoHeight??0},mediaEvents:[`resize`]},mediaPaused:{get(e){let{media:t}=e;return t?.paused??!0},set(e,t){var n;let{media:r}=t;r&&(e?r.pause():(n=r.play())==null||n.catch(()=>{}))},mediaEvents:[`play`,`playing`,`pause`,`emptied`]},mediaHasPlayed:{get(e,t){let{media:n}=e;return n?t?t.type===`playing`:!n.paused:!1},mediaEvents:[`playing`,`emptied`]},mediaEnded:{get(e){let{media:t}=e;return t?.ended??!1},mediaEvents:[`seeked`,`ended`,`emptied`]},mediaPlaybackRate:{get(e){let{media:t}=e;return t?.playbackRate??1},set(e,t){let{media:n}=t;n&&Number.isFinite(+e)&&(n.playbackRate=+e)},mediaEvents:[`ratechange`,`loadstart`]},mediaMuted:{get(e){let{media:t}=e;return t?.muted??!1},set(e,t){let{media:n,options:{noMutedPref:r}={}}=t;if(n){n.muted=e;try{let t=C.localStorage.getItem(`media-chrome-pref-muted`)!==null,i=n.hasAttribute(`muted`);if(r){t&&C.localStorage.removeItem(`media-chrome-pref-muted`);return}if(i&&!t)return;C.localStorage.setItem(`media-chrome-pref-muted`,e?`true`:`false`)}catch(e){console.debug(`Error setting muted pref`,e)}}},mediaEvents:[`volumechange`],stateOwnersUpdateHandlers:[(e,t)=>{let{options:{noMutedPref:n}}=t,{media:r}=t;if(!(!r||r.muted||n))try{let n=C.localStorage.getItem(`media-chrome-pref-muted`)===`true`;kn.mediaMuted.set(n,t),e(n)}catch(e){console.debug(`Error getting muted pref`,e)}}]},mediaLoop:{get(e){let{media:t}=e;return t?.loop},set(e,t){let{media:n}=t;n&&(n.loop=e)},mediaEvents:[`medialooprequest`]},mediaVolume:{get(e){let{media:t}=e;return t?.volume??1},set(e,t){let{media:n,options:{noVolumePref:r}={}}=t;if(n){try{e==null?C.localStorage.removeItem(`media-chrome-pref-volume`):!n.hasAttribute(`muted`)&&!r&&C.localStorage.setItem(`media-chrome-pref-volume`,e.toString())}catch(e){console.debug(`Error setting volume pref`,e)}Number.isFinite(+e)&&(n.volume=+e)}},mediaEvents:[`volumechange`],stateOwnersUpdateHandlers:[(e,t)=>{let{options:{noVolumePref:n}}=t;if(!n)try{let{media:n}=t;if(!n)return;let r=C.localStorage.getItem(`media-chrome-pref-volume`);if(r==null)return;kn.mediaVolume.set(+r,t),e(+r)}catch(e){console.debug(`Error getting volume pref`,e)}}]},mediaVolumeLevel:{get(e){let{media:t}=e;return t?.volume===void 0?`high`:t.muted||t.volume===0?`off`:t.volume<.5?`low`:t.volume<.75?`medium`:`high`},mediaEvents:[`volumechange`]},mediaCurrentTime:{get(e){let{media:t}=e;return t?.currentTime??0},set(e,t){let{media:n}=t;!n||!zt(e)||(n.currentTime=e)},mediaEvents:[`timeupdate`,`loadedmetadata`]},mediaDuration:{get(e){let{media:t,options:{defaultDuration:n}={}}=e;return n&&(!t||!t.duration||Number.isNaN(t.duration)||!Number.isFinite(t.duration))?n:Number.isFinite(t?.duration)?t.duration:NaN},mediaEvents:[`durationchange`,`loadedmetadata`,`emptied`]},mediaLoading:{get(e){let{media:t}=e;return t?.readyState<3},mediaEvents:[`waiting`,`playing`,`emptied`]},mediaSeekable:{get(e){let{media:t}=e;if(!t?.seekable?.length)return;let n=t.seekable.start(0),r=t.seekable.end(t.seekable.length-1);if(!(!n&&!r))return[Number(n.toFixed(3)),Number(r.toFixed(3))]},mediaEvents:[`loadedmetadata`,`emptied`,`progress`,`seekablechange`]},mediaBuffered:{get(e){let{media:t}=e,n=t?.buffered??[];return Array.from(n).map((e,t)=>[Number(n.start(t).toFixed(3)),Number(n.end(t).toFixed(3))])},mediaEvents:[`progress`,`emptied`]},mediaStreamType:{get(e){let{media:t,options:{defaultStreamType:n}={}}=e,r=[m.LIVE,m.ON_DEMAND].includes(n)?n:void 0;if(!t)return r;let{streamType:i}=t;if(Cn.includes(i))return i===m.UNKNOWN?r:i;let a=t.duration;return a===1/0?m.LIVE:Number.isFinite(a)?m.ON_DEMAND:r},mediaEvents:[`emptied`,`durationchange`,`loadedmetadata`,`streamtypechange`]},mediaTargetLiveWindow:{get(e){let{media:t}=e;if(!t)return NaN;let{targetLiveWindow:n}=t,r=kn.mediaStreamType.get(e);return(n==null||Number.isNaN(n))&&r===m.LIVE?0:n},mediaEvents:[`emptied`,`durationchange`,`loadedmetadata`,`streamtypechange`,`targetlivewindowchange`]},mediaTimeIsLive:{get(e){let{media:t,options:{liveEdgeOffset:n=10}={}}=e;if(!t)return!1;if(typeof t.liveEdgeStart==`number`)return!Number.isNaN(t.liveEdgeStart)&&t.currentTime>=t.liveEdgeStart;if(kn.mediaStreamType.get(e)!==m.LIVE)return!1;let r=t.seekable;if(!r)return!0;if(!r.length)return!1;let i=r.end(r.length-1)-n;return t.currentTime>=i},mediaEvents:[`playing`,`timeupdate`,`progress`,`waiting`,`emptied`]},mediaSubtitlesList:{get(e){return vn(e).map(({kind:e,label:t,language:n})=>({kind:e,label:t,language:n}))},mediaEvents:[`loadstart`],textTracksEvents:[`addtrack`,`removetrack`]},mediaSubtitlesShowing:{get(e){return yn(e).map(({kind:e,label:t,language:n})=>({kind:e,label:t,language:n}))},mediaEvents:[`loadstart`],textTracksEvents:[`addtrack`,`removetrack`,`change`],stateOwnersUpdateHandlers:[(e,t)=>{var n,r;let{media:i,options:a}=t;if(!i)return;let o=e=>{a.defaultSubtitles&&(e&&![u.CAPTIONS,u.SUBTITLES].includes(e?.track?.kind)||bn(t,!0))};return i.addEventListener(`loadstart`,o),(n=i.textTracks)==null||n.addEventListener(`addtrack`,o),(r=i.textTracks)==null||r.addEventListener(`removetrack`,o),()=>{var e,t;i.removeEventListener(`loadstart`,o),(e=i.textTracks)==null||e.removeEventListener(`addtrack`,o),(t=i.textTracks)==null||t.removeEventListener(`removetrack`,o)}}]},mediaChaptersCues:{get(e){let{media:t}=e;if(!t)return[];let[n]=Xt(t,{kind:u.CHAPTERS});return Array.from(n?.cues??[]).map(({text:e,startTime:t,endTime:n})=>({text:On(e),startTime:t,endTime:n}))},mediaEvents:[`loadstart`,`loadedmetadata`],textTracksEvents:[`addtrack`,`removetrack`,`change`],stateOwnersUpdateHandlers:[(e,t)=>{let{media:n}=t;if(!n)return;let r=n.querySelector(`track[kind="chapters"][default][src]`),i=n.shadowRoot?.querySelector(`:is(video,audio) > track[kind="chapters"][default][src]`);return r?.addEventListener(`load`,e),i?.addEventListener(`load`,e),()=>{r?.removeEventListener(`load`,e),i?.removeEventListener(`load`,e)}}]},mediaIsPip:{get(e){let{media:t,documentElement:n}=e;if(!t||!n||!n.pictureInPictureElement)return!1;if(n.pictureInPictureElement===t)return!0;if(n.pictureInPictureElement instanceof HTMLMediaElement)return t.localName?.includes(`-`)?ne(t,n.pictureInPictureElement):!1;if(n.pictureInPictureElement.localName.includes(`-`)){let e=n.pictureInPictureElement.shadowRoot;for(;e?.pictureInPictureElement;){if(e.pictureInPictureElement===t)return!0;e=e.pictureInPictureElement?.shadowRoot}}return!1},set(e,t){let{media:n}=t;if(n){if(e){if(!w.pictureInPictureEnabled){console.warn(`MediaChrome: Picture-in-picture is not enabled`);return}if(!n.requestPictureInPicture){console.warn(`MediaChrome: The current media does not support picture-in-picture`);return}let e=()=>{console.warn(`MediaChrome: The media is not ready for picture-in-picture. It must have a readyState > 0.`)};n.requestPictureInPicture().catch(t=>{if(t.code===11){if(!n.src){console.warn(`MediaChrome: The media is not ready for picture-in-picture. It must have a src set.`);return}if(n.readyState===0&&n.preload===`none`){let t=()=>{n.removeEventListener(`loadedmetadata`,r),n.preload=`none`},r=()=>{n.requestPictureInPicture().catch(e),t()};n.addEventListener(`loadedmetadata`,r),n.preload=`metadata`,setTimeout(()=>{n.readyState===0&&e(),t()},1e3)}else throw t}else throw t})}else w.pictureInPictureElement&&w.exitPictureInPicture()}},mediaEvents:[`enterpictureinpicture`,`leavepictureinpicture`]},mediaRenditionList:{get(e){let{media:t}=e;return[...t?.videoRenditions??[]].map(e=>({...e}))},mediaEvents:[`emptied`,`loadstart`],videoRenditionsEvents:[`addrendition`,`removerendition`]},mediaRenditionSelected:{get(e){let{media:t}=e;return t?.videoRenditions?.[t.videoRenditions?.selectedIndex]?.id},set(e,t){let{media:n}=t;if(!n?.videoRenditions){console.warn(`MediaController: Rendition selection not supported by this media.`);return}let r=e,i=Array.prototype.findIndex.call(n.videoRenditions,e=>e.id==r);n.videoRenditions.selectedIndex!=i&&(n.videoRenditions.selectedIndex=i)},mediaEvents:[`emptied`],videoRenditionsEvents:[`addrendition`,`removerendition`,`change`]},mediaAudioTrackList:{get(e){let{media:t}=e;return[...t?.audioTracks??[]]},mediaEvents:[`emptied`,`loadstart`],audioTracksEvents:[`addtrack`,`removetrack`]},mediaAudioTrackEnabled:{get(e){let{media:t}=e;return[...t?.audioTracks??[]].find(e=>e.enabled)?.id},set(e,t){let{media:n}=t;if(!n?.audioTracks){console.warn(`MediaChrome: Audio track selection not supported by this media.`);return}let r=e;for(let e of n.audioTracks)e.enabled=r==e.id},mediaEvents:[`emptied`],audioTracksEvents:[`addtrack`,`removetrack`,`change`]},mediaIsFullscreen:{get(e){return nn(e)},set(e,t,n){var r;e?(Zt(t),n.detail&&!t.media?.inert&&((r=t.media)==null||r.focus())):$t(t)},rootEvents:[`fullscreenchange`,`webkitfullscreenchange`],mediaEvents:[`webkitbeginfullscreen`,`webkitendfullscreen`,`webkitpresentationmodechanged`]},mediaIsCasting:{get(e){let{media:t}=e;return!t?.remote||t.remote?.state===`disconnected`?!1:t.remote.state===`connected`},set(e,t){let{media:n}=t;if(n&&!(e&&n.remote?.state!==`disconnected`)&&!(!e&&n.remote?.state!==`connected`)){if(typeof n.remote.prompt!=`function`){console.warn(`MediaChrome: Casting is not supported in this environment`);return}n.remote.prompt().catch(()=>{})}},remoteEvents:[`connect`,`connecting`,`disconnect`]},mediaIsAirplaying:{get(){return!1},set(e,t){let{media:n}=t;if(n){if(!(n.webkitShowPlaybackTargetPicker&&C.WebKitPlaybackTargetAvailabilityEvent)){console.error(`MediaChrome: received a request to select AirPlay but AirPlay is not supported in this environment`);return}n.webkitShowPlaybackTargetPicker()}},mediaEvents:[`webkitcurrentplaybacktargetiswirelesschanged`]},mediaFullscreenUnavailable:{get(e){let{media:t}=e;if(!mn||!pn(t))return p.UNSUPPORTED}},mediaPipUnavailable:{get(e){let{media:t}=e;if(!hn||!fn(t))return p.UNSUPPORTED;if(t?.disablePictureInPicture)return p.UNAVAILABLE}},mediaVolumeUnavailable:{get(e){let{media:t}=e;if(wn===!1||t?.volume==null)return p.UNSUPPORTED},stateOwnersUpdateHandlers:[e=>{wn??Tn.then(t=>e(t?void 0:p.UNSUPPORTED))}]},mediaCastUnavailable:{get(e,{availability:t=`not-available`}={}){let{media:n}=e;if(!_n||!n?.remote?.state)return p.UNSUPPORTED;if(t!=null&&t!==`available`)return p.UNAVAILABLE},stateOwnersUpdateHandlers:[(e,t)=>{var n;let{media:r}=t;if(r)return r.disableRemotePlayback||r.hasAttribute(`disableremoteplayback`)||(n=r?.remote)==null||n.watchAvailability(t=>{e({availability:t?`available`:`not-available`})}).catch(t=>{t.name===`NotSupportedError`?e({availability:null}):e({availability:`not-available`})}),()=>{var e;(e=r?.remote)==null||e.cancelWatchAvailability().catch(()=>{})}}]},mediaAirplayUnavailable:{get(e,t){if(!gn)return p.UNSUPPORTED;if(t?.availability===`not-available`)return p.UNAVAILABLE},mediaEvents:[`webkitplaybacktargetavailabilitychanged`],stateOwnersUpdateHandlers:[(e,t)=>{var n;let{media:r}=t;if(r)return r.disableRemotePlayback||r.hasAttribute(`disableremoteplayback`)||(n=r?.remote)==null||n.watchAvailability(t=>{e({availability:t?`available`:`not-available`})}).catch(t=>{t.name===`NotSupportedError`?e({availability:null}):e({availability:`not-available`})}),()=>{var e;(e=r?.remote)==null||e.cancelWatchAvailability().catch(()=>{})}}]},mediaRenditionUnavailable:{get(e){let{media:t}=e;if(!t?.videoRenditions)return p.UNSUPPORTED;if(!t.videoRenditions?.length)return p.UNAVAILABLE},mediaEvents:[`emptied`,`loadstart`],videoRenditionsEvents:[`addrendition`,`removerendition`]},mediaAudioTrackUnavailable:{get(e){let{media:t}=e;if(!t?.audioTracks)return p.UNSUPPORTED;if((t.audioTracks?.length??0)<=1)return p.UNAVAILABLE},mediaEvents:[`emptied`,`loadstart`],audioTracksEvents:[`addtrack`,`removetrack`]},mediaLang:{get(e){let{options:{mediaLang:t}={}}=e;return t??`en`}}},An={[r.MEDIA_PREVIEW_REQUEST](e,t,{detail:n}){let{media:r}=t,i=n??void 0,a,o;if(r&&i!=null){let[e]=Xt(r,{kind:u.METADATA,label:`thumbnails`}),t=Array.prototype.find.call(e?.cues??[],(e,t,n)=>t===0?e.endTime>i:t===n.length-1?e.startTime<=i:e.startTime<=i&&e.endTime>i);if(t){let e=/'^(?:[a-z]+:)?\/\//i.test(t.text)?void 0:r?.querySelector(`track[label="thumbnails"]`)?.src,n=new URL(t.text,e);o=new URLSearchParams(n.hash).get(`#xywh`).split(`,`).map(e=>+e),a=n.href}}let s=e.mediaDuration.get(t),c=e.mediaChaptersCues.get(t).find((e,t,n)=>t===n.length-1&&s===e.endTime?e.startTime<=i&&e.endTime>=i:e.startTime<=i&&e.endTime>i)?.text;return n!=null&&c==null&&(c=``),{mediaPreviewTime:i,mediaPreviewImage:a,mediaPreviewCoords:o,mediaPreviewChapter:c}},[r.MEDIA_PAUSE_REQUEST](e,t){e.mediaPaused.set(!0,t)},[r.MEDIA_PLAY_REQUEST](e,t){let n=e.mediaStreamType.get(t)===m.LIVE,r=!t.options?.noAutoSeekToLive,i=e.mediaTargetLiveWindow.get(t)>0;if(n&&r&&!i){let n=e.mediaSeekable.get(t)?.[1];if(n){let r=n-(t.options?.seekToLiveOffset??0);e.mediaCurrentTime.set(r,t)}}e.mediaPaused.set(!1,t)},[r.MEDIA_PLAYBACK_RATE_REQUEST](e,t,{detail:n}){let r=n;e.mediaPlaybackRate.set(r,t)},[r.MEDIA_MUTE_REQUEST](e,t){e.mediaMuted.set(!0,t)},[r.MEDIA_UNMUTE_REQUEST](e,t){e.mediaVolume.get(t)||e.mediaVolume.set(.25,t),e.mediaMuted.set(!1,t)},[r.MEDIA_LOOP_REQUEST](e,t,{detail:n}){let r=!!n;return e.mediaLoop.set(r,t),{mediaLoop:r}},[r.MEDIA_VOLUME_REQUEST](e,t,{detail:n}){let r=n;r&&e.mediaMuted.get(t)&&e.mediaMuted.set(!1,t),e.mediaVolume.set(r,t)},[r.MEDIA_SEEK_REQUEST](e,t,{detail:n}){let r=n;e.mediaCurrentTime.set(r,t)},[r.MEDIA_SEEK_TO_LIVE_REQUEST](e,t){let n=e.mediaSeekable.get(t)?.[1];if(Number.isNaN(Number(n)))return;let r=n-(t.options?.seekToLiveOffset??0);e.mediaCurrentTime.set(r,t)},[r.MEDIA_SHOW_SUBTITLES_REQUEST](e,t,{detail:n}){let{options:r}=t,i=vn(t),a=Wt(n),o=a[0]?.language;o&&!r.noSubtitlesLangPref&&C.localStorage.setItem(`media-chrome-pref-subtitles-lang`,o),Yt(d.SHOWING,i,a)},[r.MEDIA_DISABLE_SUBTITLES_REQUEST](e,t,{detail:n}){let r=vn(t),i=n??[];Yt(d.DISABLED,r,i)},[r.MEDIA_TOGGLE_SUBTITLES_REQUEST](e,t,{detail:n}){bn(t,n)},[r.MEDIA_RENDITION_REQUEST](e,t,{detail:n}){let r=n;e.mediaRenditionSelected.set(r,t)},[r.MEDIA_AUDIO_TRACK_REQUEST](e,t,{detail:n}){let r=n;e.mediaAudioTrackEnabled.set(r,t)},[r.MEDIA_ENTER_PIP_REQUEST](e,t){e.mediaIsFullscreen.get(t)&&e.mediaIsFullscreen.set(!1,t),e.mediaIsPip.set(!0,t)},[r.MEDIA_EXIT_PIP_REQUEST](e,t){e.mediaIsPip.set(!1,t)},[r.MEDIA_ENTER_FULLSCREEN_REQUEST](e,t,n){e.mediaIsPip.get(t)&&e.mediaIsPip.set(!1,t),e.mediaIsFullscreen.set(!0,t,n)},[r.MEDIA_EXIT_FULLSCREEN_REQUEST](e,t){e.mediaIsFullscreen.set(!1,t)},[r.MEDIA_ENTER_CAST_REQUEST](e,t){e.mediaIsFullscreen.get(t)&&e.mediaIsFullscreen.set(!1,t),e.mediaIsCasting.set(!0,t)},[r.MEDIA_EXIT_CAST_REQUEST](e,t){e.mediaIsCasting.set(!1,t)},[r.MEDIA_AIRPLAY_REQUEST](e,t){e.mediaIsAirplaying.set(!0,t)}},jn=({media:e,fullscreenElement:t,documentElement:n,stateMediator:r=kn,requestMap:i=An,options:a={},monitorStateOwnersOnlyWithSubscriptions:o=!0})=>{let s=[],c={options:{...a}},l=Object.freeze({mediaPreviewTime:void 0,mediaPreviewImage:void 0,mediaPreviewCoords:void 0,mediaPreviewChapter:void 0}),u=e=>{e!=null&&(xn(e,l)||(l=Object.freeze({...l,...e}),s.forEach(e=>e(l))))},d=()=>{let e=Object.entries(r).reduce((e,[t,{get:n}])=>(e[t]=n(c),e),{});u(e)},f={},p,m=async(e,t)=>{let n=!!p;if(p={...c,...p??{},...e},n)return;await En(...Object.values(e));let i=s.length>0&&t===0&&o,a=c.media!==p.media,l=c.media?.textTracks!==p.media?.textTracks,m=c.media?.videoRenditions!==p.media?.videoRenditions,h=c.media?.audioTracks!==p.media?.audioTracks,g=c.media?.remote!==p.media?.remote,ee=c.documentElement!==p.documentElement,te=!!c.media&&(a||i),ne=!!c.media?.textTracks&&(l||i),_=!!c.media?.videoRenditions&&(m||i),re=!!c.media?.audioTracks&&(h||i),ie=!!c.media?.remote&&(g||i),ae=!!c.documentElement&&(ee||i),oe=te||ne||_||re||ie||ae,v=s.length===0&&t===1&&o,se=!!p.media&&(a||v),ce=!!p.media?.textTracks&&(l||v),le=!!p.media?.videoRenditions&&(m||v),ue=!!p.media?.audioTracks&&(h||v),y=!!p.media?.remote&&(g||v),b=!!p.documentElement&&(ee||v),x=se||ce||le||ue||y||b;if(!(oe||x)){Object.entries(p).forEach(([e,t])=>{c[e]=t}),d(),p=void 0;return}Object.entries(r).forEach(([e,{get:t,mediaEvents:n=[],textTracksEvents:r=[],videoRenditionsEvents:i=[],audioTracksEvents:a=[],remoteEvents:o=[],rootEvents:s=[],stateOwnersUpdateHandlers:l=[]}])=>{f[e]||(f[e]={});let d=n=>{let r=t(c,n);u({[e]:r})},m;m=f[e].mediaEvents,n.forEach(t=>{m&&te&&(c.media.removeEventListener(t,m),f[e].mediaEvents=void 0),se&&(p.media.addEventListener(t,d),f[e].mediaEvents=d)}),m=f[e].textTracksEvents,r.forEach(t=>{var n,r;m&&ne&&((n=c.media.textTracks)==null||n.removeEventListener(t,m),f[e].textTracksEvents=void 0),ce&&((r=p.media.textTracks)==null||r.addEventListener(t,d),f[e].textTracksEvents=d)}),m=f[e].videoRenditionsEvents,i.forEach(t=>{var n,r;m&&_&&((n=c.media.videoRenditions)==null||n.removeEventListener(t,m),f[e].videoRenditionsEvents=void 0),le&&((r=p.media.videoRenditions)==null||r.addEventListener(t,d),f[e].videoRenditionsEvents=d)}),m=f[e].audioTracksEvents,a.forEach(t=>{var n,r;m&&re&&((n=c.media.audioTracks)==null||n.removeEventListener(t,m),f[e].audioTracksEvents=void 0),ue&&((r=p.media.audioTracks)==null||r.addEventListener(t,d),f[e].audioTracksEvents=d)}),m=f[e].remoteEvents,o.forEach(t=>{var n,r;m&&ie&&((n=c.media.remote)==null||n.removeEventListener(t,m),f[e].remoteEvents=void 0),y&&((r=p.media.remote)==null||r.addEventListener(t,d),f[e].remoteEvents=d)}),m=f[e].rootEvents,s.forEach(t=>{m&&ae&&(c.documentElement.removeEventListener(t,m),f[e].rootEvents=void 0),b&&(p.documentElement.addEventListener(t,d),f[e].rootEvents=d)});let h=f[e].stateOwnersUpdateHandlers;if(h&&oe&&(Array.isArray(h)?h:[h]).forEach(e=>{typeof e==`function`&&e()}),x){let t=l.map(e=>e(d,p)).filter(e=>typeof e==`function`);f[e].stateOwnersUpdateHandlers=t.length===1?t[0]:t}else oe&&(f[e].stateOwnersUpdateHandlers=void 0)}),Object.entries(p).forEach(([e,t])=>{c[e]=t}),d(),p=void 0};return m({media:e,fullscreenElement:t,documentElement:n,options:a}),{dispatch(e){let{type:t,detail:n}=e;if(i[t]&&l.mediaErrorCode==null){u(i[t](r,c,e));return}t===`mediaelementchangerequest`?m({media:n}):t===`fullscreenelementchangerequest`?m({fullscreenElement:n}):t===`documentelementchangerequest`?m({documentElement:n}):t===`optionschangerequest`&&(Object.entries(n??{}).forEach(([e,t])=>{c.options[e]=t}),d())},getState(){return l},subscribe(e){return m({},s.length+1),s.push(e),e(l),()=>{let t=s.indexOf(e);t>=0&&(m({},s.length-1),s.splice(t,1))}}}},Mn=(e,t,n)=>{if(!t.has(e))throw TypeError(`Cannot `+n)},W=(e,t,n)=>(Mn(e,t,`read from private field`),n?n.call(e):t.get(e)),G=(e,t,n)=>{if(t.has(e))throw TypeError(`Cannot add the same private member more than once`);t instanceof WeakSet?t.add(e):t.set(e,n)},K=(e,t,n,r)=>(Mn(e,t,`write to private field`),r?r.call(e,n):t.set(e,n),n),Nn=(e,t,n)=>(Mn(e,t,`access private method`),n),q,Pn,J,Y,Fn,X,In,Ln,Rn,zn,Z,Bn,Vn,Hn,Un,Wn=[`ArrowLeft`,`ArrowRight`,`ArrowUp`,`ArrowDown`,`Enter`,` `,`f`,`m`,`k`,`c`,`l`,`j`,`>`,`<`,`p`],Gn=10,Kn=.025,qn=.25,Jn=.25,Yn=2,Q={DEFAULT_SUBTITLES:`defaultsubtitles`,DEFAULT_STREAM_TYPE:`defaultstreamtype`,DEFAULT_DURATION:`defaultduration`,FULLSCREEN_ELEMENT:`fullscreenelement`,HOTKEYS:`hotkeys`,KEYBOARD_BACKWARD_SEEK_OFFSET:`keyboardbackwardseekoffset`,KEYBOARD_FORWARD_SEEK_OFFSET:`keyboardforwardseekoffset`,KEYBOARD_DOWN_VOLUME_STEP:`keyboarddownvolumestep`,KEYBOARD_UP_VOLUME_STEP:`keyboardupvolumestep`,KEYS_USED:`keysused`,LANG:`lang`,LOOP:`loop`,LIVE_EDGE_OFFSET:`liveedgeoffset`,NO_AUTO_SEEK_TO_LIVE:`noautoseektolive`,NO_DEFAULT_STORE:`nodefaultstore`,NO_HOTKEYS:`nohotkeys`,NO_MUTED_PREF:`nomutedpref`,NO_SUBTITLES_LANG_PREF:`nosubtitleslangpref`,NO_VOLUME_PREF:`novolumepref`,SEEK_TO_LIVE_OFFSET:`seektoliveoffset`},Xn=class extends Tt{constructor(){super(),G(this,Rn),G(this,Bn),G(this,Hn),this.mediaStateReceivers=[],this.associatedElementSubscriptions=new Map,G(this,q,new Mt(this,Q.HOTKEYS)),G(this,Pn,void 0),G(this,J,void 0),G(this,Y,null),G(this,Fn,void 0),G(this,X,void 0),G(this,In,e=>{var t;(t=W(this,J))==null||t.dispatch(e)}),G(this,Ln,void 0),G(this,Z,e=>{let{key:t,shiftKey:n}=e;if(!(n&&(t===`/`||t===`?`)||Wn.includes(t))){this.removeEventListener(`keyup`,W(this,Z));return}this.keyboardShortcutHandler(e)}),this.associateElement(this);let e={};K(this,Fn,t=>{Object.entries(t).forEach(([t,n])=>{if(t in e&&e[t]===n)return;this.propagateMediaState(t,n);let r=t.toLowerCase(),i=new C.CustomEvent(l[r],{composed:!0,detail:n});this.dispatchEvent(i)}),e=t})}static get observedAttributes(){return super.observedAttributes.concat(Q.NO_HOTKEYS,Q.HOTKEYS,Q.DEFAULT_STREAM_TYPE,Q.DEFAULT_SUBTITLES,Q.DEFAULT_DURATION,Q.NO_MUTED_PREF,Q.NO_VOLUME_PREF,Q.LANG,Q.LOOP,Q.LIVE_EDGE_OFFSET,Q.SEEK_TO_LIVE_OFFSET,Q.NO_AUTO_SEEK_TO_LIVE)}get mediaStore(){return W(this,J)}set mediaStore(e){var t;if(W(this,J)&&((t=W(this,X))==null||t.call(this),K(this,X,void 0)),K(this,J,e),!W(this,J)&&!this.hasAttribute(Q.NO_DEFAULT_STORE)){Nn(this,Rn,zn).call(this);return}K(this,X,W(this,J)?.subscribe(W(this,Fn)))}get fullscreenElement(){return W(this,Pn)??this}set fullscreenElement(e){var t;this.hasAttribute(Q.FULLSCREEN_ELEMENT)&&this.removeAttribute(Q.FULLSCREEN_ELEMENT),K(this,Pn,e),(t=W(this,J))==null||t.dispatch({type:`fullscreenelementchangerequest`,detail:this.fullscreenElement})}get defaultSubtitles(){return y(this,Q.DEFAULT_SUBTITLES)}set defaultSubtitles(e){b(this,Q.DEFAULT_SUBTITLES,e)}get defaultStreamType(){return x(this,Q.DEFAULT_STREAM_TYPE)}set defaultStreamType(e){S(this,Q.DEFAULT_STREAM_TYPE,e)}get defaultDuration(){return le(this,Q.DEFAULT_DURATION)}set defaultDuration(e){ue(this,Q.DEFAULT_DURATION,e)}get noHotkeys(){return y(this,Q.NO_HOTKEYS)}set noHotkeys(e){b(this,Q.NO_HOTKEYS,e)}get keysUsed(){return x(this,Q.KEYS_USED)}set keysUsed(e){S(this,Q.KEYS_USED,e)}get liveEdgeOffset(){return le(this,Q.LIVE_EDGE_OFFSET)}set liveEdgeOffset(e){ue(this,Q.LIVE_EDGE_OFFSET,e)}get noAutoSeekToLive(){return y(this,Q.NO_AUTO_SEEK_TO_LIVE)}set noAutoSeekToLive(e){b(this,Q.NO_AUTO_SEEK_TO_LIVE,e)}get noVolumePref(){return y(this,Q.NO_VOLUME_PREF)}set noVolumePref(e){b(this,Q.NO_VOLUME_PREF,e)}get noMutedPref(){return y(this,Q.NO_MUTED_PREF)}set noMutedPref(e){b(this,Q.NO_MUTED_PREF,e)}get noSubtitlesLangPref(){return y(this,Q.NO_SUBTITLES_LANG_PREF)}set noSubtitlesLangPref(e){b(this,Q.NO_SUBTITLES_LANG_PREF,e)}get noDefaultStore(){return y(this,Q.NO_DEFAULT_STORE)}set noDefaultStore(e){b(this,Q.NO_DEFAULT_STORE,e)}get resolvedLang(){return t()}attributeChangedCallback(e,t,i){var a,o,s,c,l,u,d,f,p,m;if(super.attributeChangedCallback(e,t,i),e===Q.NO_HOTKEYS)i!==t&&i===``?(this.hasAttribute(Q.HOTKEYS)&&console.warn("Media Chrome: Both `hotkeys` and `nohotkeys` have been set. All hotkeys will be disabled."),this.disableHotkeys()):i!==t&&i===null&&this.enableHotkeys();else if(e===Q.HOTKEYS)W(this,q).value=i;else if(e===Q.DEFAULT_SUBTITLES&&i!==t)(a=W(this,J))==null||a.dispatch({type:`optionschangerequest`,detail:{defaultSubtitles:this.hasAttribute(Q.DEFAULT_SUBTITLES)}});else if(e===Q.DEFAULT_STREAM_TYPE)(o=W(this,J))==null||o.dispatch({type:`optionschangerequest`,detail:{defaultStreamType:this.getAttribute(Q.DEFAULT_STREAM_TYPE)??void 0}});else if(e===Q.LIVE_EDGE_OFFSET&&i!==t)(s=W(this,J))==null||s.dispatch({type:`optionschangerequest`,detail:{liveEdgeOffset:this.hasAttribute(Q.LIVE_EDGE_OFFSET)?+this.getAttribute(Q.LIVE_EDGE_OFFSET):void 0,seekToLiveOffset:this.hasAttribute(Q.SEEK_TO_LIVE_OFFSET)?+this.getAttribute(Q.SEEK_TO_LIVE_OFFSET):this.hasAttribute(Q.LIVE_EDGE_OFFSET)?+this.getAttribute(Q.LIVE_EDGE_OFFSET):void 0}});else if(e===Q.SEEK_TO_LIVE_OFFSET&&i!==t)(c=W(this,J))==null||c.dispatch({type:`optionschangerequest`,detail:{seekToLiveOffset:this.hasAttribute(Q.SEEK_TO_LIVE_OFFSET)?+this.getAttribute(Q.SEEK_TO_LIVE_OFFSET):this.hasAttribute(Q.LIVE_EDGE_OFFSET)?+this.getAttribute(Q.LIVE_EDGE_OFFSET):void 0}});else if(e===Q.NO_AUTO_SEEK_TO_LIVE)(l=W(this,J))==null||l.dispatch({type:`optionschangerequest`,detail:{noAutoSeekToLive:this.hasAttribute(Q.NO_AUTO_SEEK_TO_LIVE)}});else if(e===Q.FULLSCREEN_ELEMENT){let e=i?this.getRootNode()?.getElementById(i):void 0;K(this,Pn,e),(u=W(this,J))==null||u.dispatch({type:`fullscreenelementchangerequest`,detail:this.fullscreenElement})}else e===Q.LANG&&i!==t?(n(i),(d=W(this,J))==null||d.dispatch({type:`optionschangerequest`,detail:{mediaLang:i}})):e===Q.LOOP&&i!==t?(f=W(this,J))==null||f.dispatch({type:r.MEDIA_LOOP_REQUEST,detail:i!=null}):e===Q.NO_VOLUME_PREF&&i!==t?(p=W(this,J))==null||p.dispatch({type:`optionschangerequest`,detail:{noVolumePref:this.hasAttribute(Q.NO_VOLUME_PREF)}}):e===Q.NO_MUTED_PREF&&i!==t&&((m=W(this,J))==null||m.dispatch({type:`optionschangerequest`,detail:{noMutedPref:this.hasAttribute(Q.NO_MUTED_PREF)}}))}connectedCallback(){var e,t;this.associateElement(this),!W(this,J)&&!this.hasAttribute(Q.NO_DEFAULT_STORE)&&Nn(this,Rn,zn).call(this),(e=W(this,J))==null||e.dispatch({type:`documentelementchangerequest`,detail:w}),(t=W(this,J))==null||t.dispatch({type:`fullscreenelementchangerequest`,detail:this.fullscreenElement}),super.connectedCallback(),W(this,J)&&!W(this,X)&&K(this,X,W(this,J)?.subscribe(W(this,Fn))),W(this,Ln)!==void 0&&W(this,J)&&this.media&&setTimeout(()=>{var e;this.media?.textTracks?.length&&((e=W(this,J))==null||e.dispatch({type:r.MEDIA_TOGGLE_SUBTITLES_REQUEST,detail:W(this,Ln)}))},0),this.hasAttribute(Q.NO_HOTKEYS)?this.disableHotkeys():this.enableHotkeys()}disconnectedCallback(){var e,t,n,i,a;if((e=super.disconnectedCallback)==null||e.call(this),this.disableHotkeys(),W(this,J)){let e=W(this,J).getState();K(this,Ln,!!e.mediaSubtitlesShowing?.length),(t=W(this,J))==null||t.dispatch({type:`fullscreenelementchangerequest`,detail:void 0}),(n=W(this,J))==null||n.dispatch({type:`documentelementchangerequest`,detail:void 0}),(i=W(this,J))==null||i.dispatch({type:r.MEDIA_TOGGLE_SUBTITLES_REQUEST,detail:!1})}W(this,X)&&((a=W(this,X))==null||a.call(this),K(this,X,void 0)),this.unassociateElement(this),W(this,Y)&&(W(this,Y).remove(),K(this,Y,null))}mediaSetCallback(e){var t;super.mediaSetCallback(e),(t=W(this,J))==null||t.dispatch({type:`mediaelementchangerequest`,detail:e}),e.hasAttribute(`tabindex`)||(e.tabIndex=-1)}mediaUnsetCallback(e){var t;super.mediaUnsetCallback(e),(t=W(this,J))==null||t.dispatch({type:`mediaelementchangerequest`,detail:void 0})}propagateMediaState(e,t){or(this.mediaStateReceivers,e,t)}associateElement(e){if(!e)return;let{associatedElementSubscriptions:t}=this;if(t.has(e))return;let n=sr(e,this.registerMediaStateReceiver.bind(this),this.unregisterMediaStateReceiver.bind(this));Object.values(r).forEach(t=>{e.addEventListener(t,W(this,In))}),t.set(e,n)}unassociateElement(e){if(!e)return;let{associatedElementSubscriptions:t}=this;t.has(e)&&(t.get(e)(),t.delete(e),Object.values(r).forEach(t=>{e.removeEventListener(t,W(this,In))}))}registerMediaStateReceiver(e){if(!e)return;let t=this.mediaStateReceivers;t.indexOf(e)>-1||(t.push(e),W(this,J)&&Object.entries(W(this,J).getState()).forEach(([t,n])=>{or([e],t,n)}))}unregisterMediaStateReceiver(e){let t=this.mediaStateReceivers,n=t.indexOf(e);n<0||t.splice(n,1)}enableHotkeys(){this.addEventListener(`keydown`,Nn(this,Bn,Vn))}disableHotkeys(){this.removeEventListener(`keydown`,Nn(this,Bn,Vn)),this.removeEventListener(`keyup`,W(this,Z))}get hotkeys(){return W(this,q)}set hotkeys(e){S(this,Q.HOTKEYS,e)}keyboardShortcutHandler(e){let t=e.target;if((t.getAttribute(Q.KEYS_USED)?.split(` `)??t?.keysUsed??[]).map(e=>e===`Space`?` `:e).filter(Boolean).includes(e.key))return;let n,i,a;if(!W(this,q).contains(`no${e.key.toLowerCase()}`)&&!(e.key===` `&&W(this,q).contains(`nospace`))&&!(e.shiftKey&&(e.key===`/`||e.key===`?`)&&W(this,q).contains(`noshift+/`)))switch(e.key){case` `:case`k`:n=W(this,J).getState().mediaPaused?r.MEDIA_PLAY_REQUEST:r.MEDIA_PAUSE_REQUEST,this.dispatchEvent(new C.CustomEvent(n,{composed:!0,bubbles:!0}));break;case`m`:n=this.mediaStore.getState().mediaVolumeLevel===`off`?r.MEDIA_UNMUTE_REQUEST:r.MEDIA_MUTE_REQUEST,this.dispatchEvent(new C.CustomEvent(n,{composed:!0,bubbles:!0}));break;case`f`:n=this.mediaStore.getState().mediaIsFullscreen?r.MEDIA_EXIT_FULLSCREEN_REQUEST:r.MEDIA_ENTER_FULLSCREEN_REQUEST,this.dispatchEvent(new C.CustomEvent(n,{composed:!0,bubbles:!0}));break;case`c`:this.dispatchEvent(new C.CustomEvent(r.MEDIA_TOGGLE_SUBTITLES_REQUEST,{composed:!0,bubbles:!0}));break;case`ArrowLeft`:case`j`:{let e=this.hasAttribute(Q.KEYBOARD_BACKWARD_SEEK_OFFSET)?+this.getAttribute(Q.KEYBOARD_BACKWARD_SEEK_OFFSET):Gn;i=Math.max((this.mediaStore.getState().mediaCurrentTime??0)-e,0),a=new C.CustomEvent(r.MEDIA_SEEK_REQUEST,{composed:!0,bubbles:!0,detail:i}),this.dispatchEvent(a);break}case`ArrowRight`:case`l`:{let e=this.hasAttribute(Q.KEYBOARD_FORWARD_SEEK_OFFSET)?+this.getAttribute(Q.KEYBOARD_FORWARD_SEEK_OFFSET):Gn;i=Math.max((this.mediaStore.getState().mediaCurrentTime??0)+e,0),a=new C.CustomEvent(r.MEDIA_SEEK_REQUEST,{composed:!0,bubbles:!0,detail:i}),this.dispatchEvent(a);break}case`ArrowUp`:{let e=this.hasAttribute(Q.KEYBOARD_UP_VOLUME_STEP)?+this.getAttribute(Q.KEYBOARD_UP_VOLUME_STEP):Kn;i=Math.min((this.mediaStore.getState().mediaVolume??1)+e,1),a=new C.CustomEvent(r.MEDIA_VOLUME_REQUEST,{composed:!0,bubbles:!0,detail:i}),this.dispatchEvent(a);break}case`ArrowDown`:{let e=this.hasAttribute(Q.KEYBOARD_DOWN_VOLUME_STEP)?+this.getAttribute(Q.KEYBOARD_DOWN_VOLUME_STEP):Kn;i=Math.max((this.mediaStore.getState().mediaVolume??1)-e,0),a=new C.CustomEvent(r.MEDIA_VOLUME_REQUEST,{composed:!0,bubbles:!0,detail:i}),this.dispatchEvent(a);break}case`<`:{let e=this.mediaStore.getState().mediaPlaybackRate??1;i=Math.max(e-qn,Jn).toFixed(2),a=new C.CustomEvent(r.MEDIA_PLAYBACK_RATE_REQUEST,{composed:!0,bubbles:!0,detail:i}),this.dispatchEvent(a);break}case`>`:{let e=this.mediaStore.getState().mediaPlaybackRate??1;i=Math.min(e+qn,Yn).toFixed(2),a=new C.CustomEvent(r.MEDIA_PLAYBACK_RATE_REQUEST,{composed:!0,bubbles:!0,detail:i}),this.dispatchEvent(a);break}case`/`:case`?`:e.shiftKey&&Nn(this,Hn,Un).call(this);break;case`p`:n=this.mediaStore.getState().mediaIsPip?r.MEDIA_EXIT_PIP_REQUEST:r.MEDIA_ENTER_PIP_REQUEST,a=new C.CustomEvent(n,{composed:!0,bubbles:!0}),this.dispatchEvent(a)}}};q=new WeakMap,Pn=new WeakMap,J=new WeakMap,Y=new WeakMap,Fn=new WeakMap,X=new WeakMap,In=new WeakMap,Ln=new WeakMap,Rn=new WeakSet,zn=function(){this.mediaStore=jn({media:this.media,fullscreenElement:this.fullscreenElement,options:{defaultSubtitles:this.hasAttribute(Q.DEFAULT_SUBTITLES),defaultDuration:this.hasAttribute(Q.DEFAULT_DURATION)?+this.getAttribute(Q.DEFAULT_DURATION):void 0,defaultStreamType:this.getAttribute(Q.DEFAULT_STREAM_TYPE)??void 0,liveEdgeOffset:this.hasAttribute(Q.LIVE_EDGE_OFFSET)?+this.getAttribute(Q.LIVE_EDGE_OFFSET):void 0,seekToLiveOffset:this.hasAttribute(Q.SEEK_TO_LIVE_OFFSET)?+this.getAttribute(Q.SEEK_TO_LIVE_OFFSET):this.hasAttribute(Q.LIVE_EDGE_OFFSET)?+this.getAttribute(Q.LIVE_EDGE_OFFSET):void 0,noAutoSeekToLive:this.hasAttribute(Q.NO_AUTO_SEEK_TO_LIVE),noVolumePref:this.hasAttribute(Q.NO_VOLUME_PREF),noMutedPref:this.hasAttribute(Q.NO_MUTED_PREF),noSubtitlesLangPref:this.hasAttribute(Q.NO_SUBTITLES_LANG_PREF)}})},Z=new WeakMap,Bn=new WeakSet,Vn=function(e){let{metaKey:t,altKey:n,key:r,shiftKey:i}=e,a=i&&(r===`/`||r===`?`);if(a&&W(this,Y)?.open){this.removeEventListener(`keyup`,W(this,Z));return}if(t||n||!a&&!Wn.includes(r)){this.removeEventListener(`keyup`,W(this,Z));return}let o=e.target,s=o instanceof HTMLElement&&(o.tagName.toLowerCase()===`media-volume-range`||o.tagName.toLowerCase()===`media-time-range`);[` `,`ArrowLeft`,`ArrowRight`,`ArrowUp`,`ArrowDown`].includes(r)&&!(W(this,q).contains(`no${r.toLowerCase()}`)||r===` `&&W(this,q).contains(`nospace`))&&!s&&e.preventDefault(),this.addEventListener(`keyup`,W(this,Z),{once:!0})},Hn=new WeakSet,Un=function(){W(this,Y)||(K(this,Y,w.createElement(`media-keyboard-shortcuts-dialog`)),this.appendChild(W(this,Y))),W(this,Y).open=!0};var Zn=Object.values(s),Qn=Object.values(a),$n=e=>{var t;let{observedAttributes:n}=e.constructor;!n&&e.nodeName?.includes(`-`)&&(C.customElements.upgrade(e),{observedAttributes:n}=e.constructor);let r=((t=(e?.getAttribute)?.call(e,i.MEDIA_CHROME_ATTRIBUTES))?.split)?.call(t,/\s+/);return Array.isArray(n||r)?(n||r).filter(e=>Zn.includes(e)):[]},er=e=>(e.nodeName?.includes(`-`)&&C.customElements.get(e.nodeName?.toLowerCase())&&!(e instanceof C.customElements.get(e.nodeName.toLowerCase()))&&C.customElements.upgrade(e),Qn.some(t=>t in e)),tr=e=>er(e)||!!$n(e).length,nr=e=>(e?.join)?.call(e,`:`),rr={[s.MEDIA_SUBTITLES_LIST]:Kt,[s.MEDIA_SUBTITLES_SHOWING]:Kt,[s.MEDIA_SEEKABLE]:nr,[s.MEDIA_BUFFERED]:e=>e?.map(nr).join(` `),[s.MEDIA_PREVIEW_COORDS]:e=>e?.join(` `),[s.MEDIA_RENDITION_LIST]:Nt,[s.MEDIA_AUDIO_TRACK_LIST]:Lt},ir=async(e,t,n)=>{if(e.isConnected||await Bt(0),typeof n==`boolean`||n==null)return b(e,t,n);if(typeof n==`number`)return ue(e,t,n);if(typeof n==`string`)return S(e,t,n);if(Array.isArray(n)&&!n.length)return e.removeAttribute(t);let r=rr[t]?.call(rr,n)??n;return e.setAttribute(t,r)},ar=e=>!!e.closest?.call(e,`*[slot="media"]`),$=(e,t)=>{if(ar(e))return;let n=(e,t)=>{tr(e)&&t(e);let{children:n=[]}=e??{},r=e?.shadowRoot?.children??[];[...n,...r].forEach(e=>$(e,t))},r=e?.nodeName.toLowerCase();if(r.includes(`-`)&&!tr(e)){C.customElements.whenDefined(r).then(()=>{n(e,t)});return}n(e,t)},or=(e,t,n)=>{e.forEach(e=>{if(t in e){e[t]=n;return}let r=$n(e),i=t.toLowerCase();r.includes(i)&&ir(e,i,n)})},sr=(e,t,n)=>{$(e,t);let a=e=>{t(e?.composedPath()[0]??e.target)},o=e=>{n(e?.composedPath()[0]??e.target)};e.addEventListener(r.REGISTER_MEDIA_STATE_RECEIVER,a),e.addEventListener(r.UNREGISTER_MEDIA_STATE_RECEIVER,o);let s=e=>{e.forEach(e=>{let{addedNodes:r=[],removedNodes:a=[],type:o,target:s,attributeName:c}=e;o===`childList`?(Array.prototype.forEach.call(r,e=>$(e,t)),Array.prototype.forEach.call(a,e=>$(e,n))):o===`attributes`&&c===i.MEDIA_CHROME_ATTRIBUTES&&(tr(s)?t(s):n(s))})},c=[],l=e=>{let r=e.target;r.name!==`media`&&(c.forEach(e=>$(e,n)),c=[...r.assignedElements({flatten:!0})],c.forEach(e=>$(e,t)))};e.addEventListener(`slotchange`,l);let u=new MutationObserver(s);return u.observe(e,{childList:!0,attributes:!0,subtree:!0}),()=>{$(e,n),e.removeEventListener(`slotchange`,l),u.disconnect(),e.removeEventListener(r.REGISTER_MEDIA_STATE_RECEIVER,a),e.removeEventListener(r.UNREGISTER_MEDIA_STATE_RECEIVER,o)}};C.customElements.get(`media-controller`)||C.customElements.define(`media-controller`,Xn);export{b as C,s as D,i as E,r as O,g as S,S as T,v as _,Ge as a,ce as b,C as c,re as d,te as f,le as g,ee as h,We as i,_ as l,ie as m,Pt as n,A as o,y as p,Mt as r,w as s,zt as t,ne as u,oe as v,ue as w,ae as x,x as y};