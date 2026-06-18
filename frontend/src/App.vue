<template>
  <div class="app-container">
    <div ref="canvasContainer" class="canvas-container"></div>
    <div class="sidebar">
      <h2 class="title">无人机监控</h2>
      <div class="status-bar">
        <span class="status-dot" :class="{ connected: isConnected }"></span>
        <span class="status-text">{{ isConnected ? '已连接' : '连接中...' }}</span>
      </div>
      <div class="drone-list">
        <div
          v-for="drone in droneList"
          :key="drone.id"
          class="drone-card"
          :class="{
            active: selectedDrone === drone.id,
            warning: isLowBattery(drone.battery)
          }"
          @click="selectDrone(drone.id)"
        >
          <div class="drone-header">
            <span class="drone-id" :class="{ warnid: isLowBattery(drone.battery) }">
              {{ drone.id }}
              <span v-if="isLowBattery(drone.battery)" class="warn-sign">⚠</span>
            </span>
            <span class="drone-battery" :class="batteryClass(drone.battery)">
              {{ Math.round(drone.battery) }}%
            </span>
          </div>
          <div class="battery-bar">
            <div
              class="battery-fill"
              :style="{ width: drone.battery + '%' }"
              :class="batteryClass(drone.battery)"
            ></div>
          </div>
          <div class="drone-coords">
            <span>X: {{ drone.x.toFixed(1) }}</span>
            <span>Y: {{ drone.y.toFixed(1) }}</span>
            <span>Z: {{ drone.z.toFixed(1) }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'

const canvasContainer = ref(null)
const isConnected = ref(false)
const selectedDrone = ref(null)

const droneColors = {
  'DRONE-A': 0x00ffff,
  'DRONE-B': 0xff6b6b,
  'DRONE-C': 0xffd93d,
  'DRONE-D': 0x6bcb77,
  'DRONE-E': 0x9d4edd
}

const LOW_BATTERY = 20

const droneMap = reactive({})
const droneList = computed(() => Object.values(droneMap).sort((a, b) => a.id.localeCompare(b.id)))

let scene, camera, renderer, controls
const droneMeshes = {}
const targetPositions = {}
const pendingData = {}
let animationId
let ws
let wsReconnectTimer

let sharedGeometries = {}
let sharedMaterials = {}
let disposableObjects = []

const RENDER_INTERVAL = 1000 / 30
let lastRenderSync = 0

const TRAIL_MAX_POINTS = 120
const TRAIL_ADD_INTERVAL = 0.08
const droneTrailStates = {}

let sprayCanvas, sprayCtx, sprayTexture, sprayMesh
const SPRAY_CANVAS_SIZE = 512
const FARM_SIZE = 100

function isLowBattery(battery) {
  return battery < LOW_BATTERY
}

function createSharedAssets() {
  sharedGeometries.sphere = new THREE.SphereGeometry(1.5, 24, 24)
  sharedGeometries.glow = new THREE.SphereGeometry(2.5, 24, 24)
  sharedGeometries.ring = new THREE.TorusGeometry(3, 0.1, 6, 24)
  sharedGeometries.farm = new THREE.BoxGeometry(100, 5, 100)
  sharedGeometries.tree = new THREE.ConeGeometry(1.5, 4, 6)

  Object.keys(droneColors).forEach((id) => {
    const color = droneColors[id]
    sharedMaterials[`sphere_${id}`] = new THREE.MeshStandardMaterial({
      color,
      emissive: color,
      emissiveIntensity: 0.6,
      roughness: 0.3,
      metalness: 0.7
    })
    sharedMaterials[`glow_${id}`] = new THREE.MeshBasicMaterial({
      color,
      transparent: true,
      opacity: 0.25,
      depthWrite: false
    })
    sharedMaterials[`ring_${id}`] = new THREE.MeshBasicMaterial({
      color,
      transparent: true,
      opacity: 0.6,
      depthWrite: false
    })
    sharedMaterials[`trail_${id}`] = new THREE.LineBasicMaterial({
      color,
      transparent: true,
      opacity: 0.55,
      depthWrite: false
    })
  })

  sharedMaterials.farm = new THREE.MeshStandardMaterial({
    color: 0x2d6a4f,
    roughness: 0.8,
    metalness: 0.1
  })
  sharedMaterials.farmEdge = new THREE.LineBasicMaterial({ color: 0x52b788 })
  sharedMaterials.tree = new THREE.MeshStandardMaterial({ color: 0x40916c })

  sharedMaterials.sphereWarning = new THREE.MeshStandardMaterial({
    color: 0xff1a1a,
    emissive: 0xff0000,
    emissiveIntensity: 1.2,
    roughness: 0.2,
    metalness: 0.8
  })
  sharedMaterials.glowWarning = new THREE.MeshBasicMaterial({
    color: 0xff1a1a,
    transparent: true,
    opacity: 0.45,
    depthWrite: false
  })
  sharedMaterials.ringWarning = new THREE.MeshBasicMaterial({
    color: 0xff3333,
    transparent: true,
    opacity: 0.85,
    depthWrite: false
  })
}

function initScene() {
  createSharedAssets()

  const container = canvasContainer.value
  const width = container.clientWidth
  const height = container.clientHeight

  scene = new THREE.Scene()
  scene.background = new THREE.Color(0x0a0e14)
  scene.fog = new THREE.Fog(0x0a0e14, 100, 300)

  camera = new THREE.PerspectiveCamera(60, width / height, 0.1, 1000)
  camera.position.set(80, 80, 80)

  renderer = new THREE.WebGLRenderer({ antialias: true })
  renderer.setSize(width, height)
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
  renderer.shadowMap.enabled = true
  container.appendChild(renderer.domElement)

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.05
  controls.maxPolarAngle = Math.PI / 2.1
  controls.minDistance = 30
  controls.maxDistance = 200

  addLights()
  addFarmland()
  addSprayOverlay()
  addGrid()
  animate()
}

function addLights() {
  const ambientLight = new THREE.AmbientLight(0x404060, 0.6)
  scene.add(ambientLight)
  disposableObjects.push(ambientLight)

  const directionalLight = new THREE.DirectionalLight(0xffffff, 0.9)
  directionalLight.position.set(50, 100, 50)
  directionalLight.castShadow = true
  directionalLight.shadow.mapSize.width = 1024
  directionalLight.shadow.mapSize.height = 1024
  scene.add(directionalLight)
  disposableObjects.push(directionalLight)

  const hemiLight = new THREE.HemisphereLight(0x52b788, 0x1b4332, 0.3)
  scene.add(hemiLight)
  disposableObjects.push(hemiLight)
}

function addFarmland() {
  const farm = new THREE.Mesh(sharedGeometries.farm, sharedMaterials.farm)
  farm.position.y = -2.5
  farm.receiveShadow = true
  scene.add(farm)
  disposableObjects.push(farm)

  const edgeGeometry = new THREE.EdgesGeometry(sharedGeometries.farm)
  const edges = new THREE.LineSegments(edgeGeometry, sharedMaterials.farmEdge)
  edges.position.y = -2.5
  scene.add(edges)
  disposableObjects.push(edges, edgeGeometry)

  for (let i = 0; i < 20; i++) {
    const tree = new THREE.Mesh(sharedGeometries.tree, sharedMaterials.tree)
    const angle = Math.random() * Math.PI * 2
    const radius = 45 + Math.random() * 5
    tree.position.set(
      Math.cos(angle) * radius,
      2,
      Math.sin(angle) * radius
    )
    tree.castShadow = true
    scene.add(tree)
    disposableObjects.push(tree)
  }
}

function addSprayOverlay() {
  sprayCanvas = document.createElement('canvas')
  sprayCanvas.width = SPRAY_CANVAS_SIZE
  sprayCanvas.height = SPRAY_CANVAS_SIZE
  sprayCtx = sprayCanvas.getContext('2d')

  sprayCtx.fillStyle = '#2d6a4f'
  sprayCtx.fillRect(0, 0, SPRAY_CANVAS_SIZE, SPRAY_CANVAS_SIZE)

  sprayTexture = new THREE.CanvasTexture(sprayCanvas)
  sprayTexture.wrapS = THREE.ClampToEdgeWrapping
  sprayTexture.wrapT = THREE.ClampToEdgeWrapping
  sprayTexture.needsUpdate = true

  const sprayGeometry = new THREE.PlaneGeometry(FARM_SIZE, FARM_SIZE)
  const sprayMaterial = new THREE.MeshStandardMaterial({
    map: sprayTexture,
    transparent: true,
    opacity: 0.85,
    roughness: 0.9,
    metalness: 0
  })
  sprayMesh = new THREE.Mesh(sprayGeometry, sprayMaterial)
  sprayMesh.rotation.x = -Math.PI / 2
  sprayMesh.position.y = 0.05
  sprayMesh.receiveShadow = true
  scene.add(sprayMesh)

  disposableObjects.push(sprayMesh, sprayGeometry, sprayMaterial, sprayTexture)
}

function paintSpray(droneId, worldX, worldZ, radius = 6) {
  const u = ((worldX + FARM_SIZE / 2) / FARM_SIZE) * SPRAY_CANVAS_SIZE
  const v = (1 - (worldZ + FARM_SIZE / 2) / FARM_SIZE) * SPRAY_CANVAS_SIZE
  const pxRadius = (radius / FARM_SIZE) * SPRAY_CANVAS_SIZE

  const color = droneColors[droneId]
  const r = (color >> 16) & 0xff
  const g = (color >> 8) & 0xff
  const b = color & 0xff

  const grad = sprayCtx.createRadialGradient(u, v, 0, u, v, pxRadius)
  grad.addColorStop(0, `rgba(${r}, ${g}, ${b}, 0.55)`)
  grad.addColorStop(0.5, `rgba(${r}, ${g}, ${b}, 0.25)`)
  grad.addColorStop(1, `rgba(${r}, ${g}, ${b}, 0)`)

  sprayCtx.fillStyle = grad
  sprayCtx.beginPath()
  sprayCtx.arc(u, v, pxRadius, 0, Math.PI * 2)
  sprayCtx.fill()

  sprayTexture.needsUpdate = true
}

function addGrid() {
  const gridHelper = new THREE.GridHelper(100, 20, 0x52b788, 0x1b4332)
  gridHelper.position.y = 0.01
  scene.add(gridHelper)
  disposableObjects.push(gridHelper)
}

function createDroneMesh(droneId) {
  const group = new THREE.Group()
  const baseColor = droneColors[droneId] || 0x00ffff

  const sphere = new THREE.Mesh(
    sharedGeometries.sphere,
    sharedMaterials[`sphere_${droneId}`] || sharedMaterials['sphere_DRONE-A']
  )
  sphere.castShadow = true
  group.add(sphere)

  const glow = new THREE.Mesh(
    sharedGeometries.glow,
    sharedMaterials[`glow_${droneId}`] || sharedMaterials['glow_DRONE-A']
  )
  group.add(glow)

  const ring = new THREE.Mesh(
    sharedGeometries.ring,
    sharedMaterials[`ring_${droneId}`] || sharedMaterials['ring_DRONE-A']
  )
  ring.rotation.x = Math.PI / 2
  group.add(ring)

  const trailPositions = new Float32Array(TRAIL_MAX_POINTS * 3)
  const trailGeometry = new THREE.BufferGeometry()
  trailGeometry.setAttribute('position', new THREE.BufferAttribute(trailPositions, 3))
  trailGeometry.setDrawRange(0, 0)

  const trail = new THREE.Line(
    trailGeometry,
    sharedMaterials[`trail_${droneId}`] || sharedMaterials['trail_DRONE-A']
  )
  scene.add(trail)

  group.position.set(0, 10, 0)
  scene.add(group)

  droneTrailStates[droneId] = {
    positions: trailPositions,
    geometry: trailGeometry,
    trail,
    count: 0,
    head: 0,
    lastAddedTime: 0,
    prevWorldX: 0,
    prevWorldZ: 0
  }

  return {
    group,
    sphere,
    glow,
    ring,
    baseColor,
    isWarning: false
  }
}

function addTrailPoint(droneId, x, y, z, timestamp) {
  const state = droneTrailStates[droneId]
  if (!state) return

  const elapsed = (timestamp - state.lastAddedTime) / 1000
  if (elapsed < TRAIL_ADD_INTERVAL && state.count > 0) return

  state.lastAddedTime = timestamp

  const idx = (state.head % TRAIL_MAX_POINTS) * 3
  state.positions[idx] = x
  state.positions[idx + 1] = y
  state.positions[idx + 2] = z

  state.head++
  if (state.count < TRAIL_MAX_POINTS) state.count++

  const sorted = new Float32Array(TRAIL_MAX_POINTS * 3)
  if (state.count < TRAIL_MAX_POINTS) {
    sorted.set(state.positions.subarray(0, state.count * 3))
  } else {
    const start = state.head % TRAIL_MAX_POINTS
    const part1 = state.positions.subarray(start * 3)
    const part2 = state.positions.subarray(0, start * 3)
    sorted.set(part1)
    sorted.set(part2, part1.length)
  }
  state.geometry.attributes.position.array.set(sorted)
  state.geometry.attributes.position.needsUpdate = true
  state.geometry.setDrawRange(0, state.count)

  const paintX = state.count === 1 ? x : (x + state.prevWorldX) / 2
  const paintZ = state.count === 1 ? z : (z + state.prevWorldZ) / 2
  paintSpray(droneId, paintX, paintZ, 5)

  state.prevWorldX = x
  state.prevWorldZ = z
}

function applyWarningStyle(mesh, enable) {
  if (mesh.isWarning === enable) return
  mesh.isWarning = enable

  if (enable) {
    mesh.sphere.material = sharedMaterials.sphereWarning
    mesh.glow.material = sharedMaterials.glowWarning
    mesh.ring.material = sharedMaterials.ringWarning
  } else {
    const id = findDroneIdByMesh(mesh)
    mesh.sphere.material = sharedMaterials[`sphere_${id}`] || sharedMaterials['sphere_DRONE-A']
    mesh.glow.material = sharedMaterials[`glow_${id}`] || sharedMaterials['glow_DRONE-A']
    mesh.ring.material = sharedMaterials[`ring_${id}`] || sharedMaterials['ring_DRONE-A']
  }
}

function findDroneIdByMesh(targetMesh) {
  const keys = Object.keys(droneMeshes)
  for (let i = 0; i < keys.length; i++) {
    if (droneMeshes[keys[i]] === targetMesh) return keys[i]
  }
  return 'DRONE-A'
}

function selectDrone(id) {
  selectedDrone.value = selectedDrone.value === id ? null : id
}

function batteryClass(battery) {
  if (battery < LOW_BATTERY) return 'critical'
  if (battery > 60) return 'high'
  if (battery > 30) return 'medium'
  return 'low'
}

function connectWebSocket() {
  ws = new WebSocket('ws://localhost:8080/ws')

  ws.onopen = () => {
    isConnected.value = true
    if (wsReconnectTimer) {
      clearTimeout(wsReconnectTimer)
      wsReconnectTimer = null
    }
  }

  ws.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data)
      if (Array.isArray(data)) {
        data.forEach((d) => {
          pendingData[d.id] = d
        })
      }
    } catch (e) {
      console.error('Parse WebSocket message error:', e)
    }
  }

  ws.onclose = () => {
    isConnected.value = false
    if (!wsReconnectTimer) {
      wsReconnectTimer = setTimeout(connectWebSocket, 3000)
    }
  }

  ws.onerror = () => {
    try { ws.close() } catch (e) {}
  }
}

function syncPendingToState() {
  const ids = Object.keys(pendingData)
  if (ids.length === 0) return

  for (const id of ids) {
    const d = pendingData[id]

    if (!droneMap[id]) {
      droneMap[id] = { id: d.id, x: d.x, y: d.y, z: d.z, battery: d.battery }
    } else {
      droneMap[id].x = d.x
      droneMap[id].y = d.y
      droneMap[id].z = d.z
      droneMap[id].battery = d.battery
    }

    targetPositions[id] = { x: d.x, y: d.y, z: d.z }

    if (!droneMeshes[id]) {
      droneMeshes[id] = createDroneMesh(id)
    }
  }

  for (const id of Object.keys(pendingData)) {
    delete pendingData[id]
  }
}

function animate(timestamp = 0) {
  animationId = requestAnimationFrame(animate)

  if (timestamp - lastRenderSync >= RENDER_INTERVAL) {
    syncPendingToState()
    lastRenderSync = timestamp
  }

  const ids = Object.keys(droneMeshes)
  for (let i = 0; i < ids.length; i++) {
    const id = ids[i]
    const mesh = droneMeshes[id]
    const target = targetPositions[id]

    if (target) {
      mesh.group.position.x += (target.x - mesh.group.position.x) * 0.08
      mesh.group.position.y += (target.y - mesh.group.position.y) * 0.08
      mesh.group.position.z += (target.z - mesh.group.position.z) * 0.08
    }

    mesh.ring.rotation.z += 0.02
    const pulse = 1 + Math.sin(timestamp * 0.003 + i) * 0.1
    mesh.glow.scale.setScalar(pulse)

    addTrailPoint(
      id,
      mesh.group.position.x,
      mesh.group.position.y,
      mesh.group.position.z,
      timestamp
    )

    const drone = droneMap[id]
    if (drone) {
      applyWarningStyle(mesh, drone.battery < LOW_BATTERY)
    }

    if (selectedDrone.value === id) {
      mesh.group.scale.setScalar(1.3)
    } else {
      mesh.group.scale.setScalar(1)
    }
  }

  controls.update()
  renderer.render(scene, camera)
}

function handleResize() {
  if (!canvasContainer.value || !renderer || !camera) return
  const width = canvasContainer.value.clientWidth
  const height = canvasContainer.value.clientHeight
  camera.aspect = width / height
  camera.updateProjectionMatrix()
  renderer.setSize(width, height)
}

function disposeAll() {
  Object.keys(droneMeshes).forEach((id) => {
    const { group } = droneMeshes[id]
    scene.remove(group)
  })
  Object.keys(droneTrailStates).forEach((id) => {
    const state = droneTrailStates[id]
    scene.remove(state.trail)
    state.geometry.dispose?.()
  })
  Object.keys(droneMeshes).length = 0
  Object.keys(droneTrailStates).length = 0

  disposableObjects.forEach((obj) => {
    if (obj.geometry) obj.geometry.dispose?.()
    if (obj.material) {
      if (Array.isArray(obj.material)) {
        obj.material.forEach((m) => m.dispose?.())
      } else {
        obj.material.dispose?.()
      }
    }
    if (obj.isTexture) obj.dispose?.()
    scene.remove?.(obj)
  })
  disposableObjects = []

  Object.values(sharedGeometries).forEach((g) => g.dispose?.())
  Object.values(sharedMaterials).forEach((m) => m.dispose?.())
  sharedGeometries = {}
  sharedMaterials = {}

  sprayCanvas = null
  sprayCtx = null

  controls?.dispose?.()
  renderer?.dispose?.()
  renderer?.forceContextLoss?.()

  if (renderer?.domElement?.parentNode) {
    renderer.domElement.parentNode.removeChild(renderer.domElement)
  }

  scene = null
  camera = null
  renderer = null
  controls = null
}

onMounted(() => {
  initScene()
  connectWebSocket()
  window.addEventListener('resize', handleResize, { passive: true })
})

onUnmounted(() => {
  window.removeEventListener('resize', handleResize)
  if (animationId) cancelAnimationFrame(animationId)
  if (wsReconnectTimer) clearTimeout(wsReconnectTimer)
  if (ws) {
    try { ws.close() } catch (e) {}
  }
  disposeAll()
})
</script>

<style scoped>
.app-container {
  width: 100%;
  height: 100%;
  display: flex;
  position: relative;
}

.canvas-container {
  flex: 1;
  position: relative;
}

.sidebar {
  width: 320px;
  background: rgba(13, 17, 23, 0.95);
  border-left: 1px solid rgba(82, 183, 136, 0.3);
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 20px;
  backdrop-filter: blur(10px);
}

.title {
  color: #52b788;
  font-size: 22px;
  font-weight: 600;
  text-align: center;
  letter-spacing: 2px;
}

.status-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  background: rgba(82, 183, 136, 0.1);
  border-radius: 8px;
  border: 1px solid rgba(82, 183, 136, 0.2);
}

.status-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #ff6b6b;
  animation: pulse 2s infinite;
}

.status-dot.connected {
  background: #52b788;
}

.status-text {
  color: #95d5b2;
  font-size: 14px;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}

.drone-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  flex: 1;
  overflow-y: auto;
}

.drone-card {
  background: rgba(27, 67, 50, 0.3);
  border: 1px solid rgba(82, 183, 136, 0.3);
  border-radius: 12px;
  padding: 16px;
  cursor: pointer;
  transition: all 0.3s ease;
}

.drone-card:hover {
  background: rgba(82, 183, 136, 0.15);
  border-color: rgba(82, 183, 136, 0.6);
  transform: translateX(-4px);
}

.drone-card.active {
  background: rgba(82, 183, 136, 0.2);
  border-color: #52b788;
  box-shadow: 0 0 20px rgba(82, 183, 136, 0.3);
}

.drone-card.warning {
  background: rgba(255, 26, 26, 0.15);
  border-color: rgba(255, 51, 51, 0.7);
  animation: warning-blink 0.9s ease-in-out infinite;
}

.drone-card.warning:hover {
  background: rgba(255, 51, 51, 0.25);
  border-color: #ff3333;
}

@keyframes warning-blink {
  0%, 100% {
    box-shadow: 0 0 0 rgba(255, 51, 51, 0);
  }
  50% {
    box-shadow: 0 0 28px rgba(255, 51, 51, 0.75);
    background: rgba(255, 51, 51, 0.28);
  }
}

.drone-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.drone-id {
  color: #d8f3dc;
  font-size: 16px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 6px;
}

.drone-id.warnid {
  color: #ff4444;
  text-shadow: 0 0 8px rgba(255, 51, 51, 0.8);
  animation: text-flash 0.9s ease-in-out infinite;
}

@keyframes text-flash {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}

.warn-sign {
  font-size: 18px;
  animation: warn-shake 0.45s ease-in-out infinite;
}

@keyframes warn-shake {
  0%, 100% { transform: rotate(-8deg); }
  50% { transform: rotate(8deg); }
}

.drone-battery {
  font-size: 14px;
  font-weight: 600;
  padding: 4px 10px;
  border-radius: 20px;
}

.drone-battery.high {
  color: #52b788;
  background: rgba(82, 183, 136, 0.15);
}

.drone-battery.medium {
  color: #ffd93d;
  background: rgba(255, 217, 61, 0.15);
}

.drone-battery.low {
  color: #ff9500;
  background: rgba(255, 149, 0, 0.15);
}

.drone-battery.critical {
  color: #ff3333;
  background: rgba(255, 51, 51, 0.22);
  animation: bat-crit-flash 0.9s ease-in-out infinite;
}

@keyframes bat-crit-flash {
  0%, 100% { transform: scale(1); }
  50% { transform: scale(1.08); }
}

.battery-bar {
  width: 100%;
  height: 8px;
  background: rgba(0, 0, 0, 0.4);
  border-radius: 4px;
  overflow: hidden;
  margin-bottom: 12px;
}

.battery-fill {
  height: 100%;
  transition: width 0.5s ease;
  border-radius: 4px;
}

.battery-fill.high {
  background: linear-gradient(90deg, #40916c, #52b788);
}

.battery-fill.medium {
  background: linear-gradient(90deg, #e9c46a, #ffd93d);
}

.battery-fill.low {
  background: linear-gradient(90deg, #f4a261, #ff9500);
}

.battery-fill.critical {
  background: linear-gradient(90deg, #e63946, #ff1a1a);
  animation: fill-pulse 0.9s ease-in-out infinite;
}

@keyframes fill-pulse {
  0%, 100% { filter: brightness(1); }
  50% { filter: brightness(1.6); }
}

.drone-coords {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: #74c69d;
  font-family: 'Monaco', 'Consolas', monospace;
}

.drone-list::-webkit-scrollbar {
  width: 6px;
}

.drone-list::-webkit-scrollbar-track {
  background: rgba(0, 0, 0, 0.2);
  border-radius: 3px;
}

.drone-list::-webkit-scrollbar-thumb {
  background: rgba(82, 183, 136, 0.4);
  border-radius: 3px;
}

.drone-list::-webkit-scrollbar-thumb:hover {
  background: rgba(82, 183, 136, 0.6);
}
</style>
