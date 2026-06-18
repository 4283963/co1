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
          v-for="drone in drones"
          :key="drone.id"
          class="drone-card"
          :class="{ active: selectedDrone === drone.id }"
          @click="selectDrone(drone.id)"
        >
          <div class="drone-header">
            <span class="drone-id">{{ drone.id }}</span>
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
import { ref, onMounted, onUnmounted, reactive } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'

const canvasContainer = ref(null)
const isConnected = ref(false)
const selectedDrone = ref(null)
const drones = reactive([])

let scene, camera, renderer, controls
let droneMeshes = {}
let targetPositions = {}
let animationId
let ws

const droneColors = {
  'DRONE-A': 0x00ffff,
  'DRONE-B': 0xff6b6b,
  'DRONE-C': 0xffd93d,
  'DRONE-D': 0x6bcb77,
  'DRONE-E': 0x9d4edd
}

function initScene() {
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
  addGrid()
  animate()
}

function addLights() {
  const ambientLight = new THREE.AmbientLight(0x404060, 0.6)
  scene.add(ambientLight)

  const directionalLight = new THREE.DirectionalLight(0xffffff, 0.8)
  directionalLight.position.set(50, 100, 50)
  directionalLight.castShadow = true
  directionalLight.shadow.mapSize.width = 2048
  directionalLight.shadow.mapSize.height = 2048
  scene.add(directionalLight)

  const pointLight = new THREE.PointLight(0x00ff88, 0.5, 200)
  pointLight.position.set(0, 50, 0)
  scene.add(pointLight)
}

function addFarmland() {
  const farmGeometry = new THREE.BoxGeometry(100, 5, 100)
  const farmMaterial = new THREE.MeshStandardMaterial({
    color: 0x2d6a4f,
    roughness: 0.8,
    metalness: 0.1
  })
  const farm = new THREE.Mesh(farmGeometry, farmMaterial)
  farm.position.y = -2.5
  farm.receiveShadow = true
  scene.add(farm)

  const edgeGeometry = new THREE.EdgesGeometry(farmGeometry)
  const edgeMaterial = new THREE.LineBasicMaterial({ color: 0x52b788 })
  const edges = new THREE.LineSegments(edgeGeometry, edgeMaterial)
  edges.position.y = -2.5
  scene.add(edges)

  for (let i = 0; i < 20; i++) {
    const treeGeometry = new THREE.ConeGeometry(1.5, 4, 6)
    const treeMaterial = new THREE.MeshStandardMaterial({
      color: 0x40916c
    })
    const tree = new THREE.Mesh(treeGeometry, treeMaterial)
    const angle = Math.random() * Math.PI * 2
    const radius = 45 + Math.random() * 5
    tree.position.set(
      Math.cos(angle) * radius,
      2,
      Math.sin(angle) * radius
    )
    tree.castShadow = true
    scene.add(tree)
  }
}

function addGrid() {
  const gridHelper = new THREE.GridHelper(100, 20, 0x52b788, 0x1b4332)
  gridHelper.position.y = 0.01
  scene.add(gridHelper)
}

function createDroneMesh(drone) {
  const group = new THREE.Group()

  const sphereGeometry = new THREE.SphereGeometry(1.5, 32, 32)
  const sphereMaterial = new THREE.MeshStandardMaterial({
    color: droneColors[drone.id] || 0x00ffff,
    emissive: droneColors[drone.id] || 0x00ffff,
    emissiveIntensity: 0.5,
    roughness: 0.3,
    metalness: 0.7
  })
  const sphere = new THREE.Mesh(sphereGeometry, sphereMaterial)
  sphere.castShadow = true
  group.add(sphere)

  const glowGeometry = new THREE.SphereGeometry(2.5, 32, 32)
  const glowMaterial = new THREE.MeshBasicMaterial({
    color: droneColors[drone.id] || 0x00ffff,
    transparent: true,
    opacity: 0.2
  })
  const glow = new THREE.Mesh(glowGeometry, glowMaterial)
  group.add(glow)

  const ringGeometry = new THREE.TorusGeometry(3, 0.1, 8, 32)
  const ringMaterial = new THREE.MeshBasicMaterial({
    color: droneColors[drone.id] || 0x00ffff,
    transparent: true,
    opacity: 0.6
  })
  const ring = new THREE.Mesh(ringGeometry, ringMaterial)
  ring.rotation.x = Math.PI / 2
  group.add(ring)

  const pointLight = new THREE.PointLight(
    droneColors[drone.id] || 0x00ffff,
    1,
    20
  )
  group.add(pointLight)

  group.position.set(drone.x, drone.y, drone.z)
  scene.add(group)

  return { group, sphere, glow, ring }
}

function selectDrone(id) {
  selectedDrone.value = selectedDrone.value === id ? null : id
}

function batteryClass(battery) {
  if (battery > 60) return 'high'
  if (battery > 30) return 'medium'
  return 'low'
}

function connectWebSocket() {
  ws = new WebSocket('ws://localhost:8080/ws')

  ws.onopen = () => {
    isConnected.value = true
    console.log('WebSocket connected')
  }

  ws.onmessage = (event) => {
    const data = JSON.parse(event.data)
    updateDrones(data)
  }

  ws.onclose = () => {
    isConnected.value = false
    console.log('WebSocket disconnected, retrying...')
    setTimeout(connectWebSocket, 3000)
  }

  ws.onerror = (error) => {
    console.error('WebSocket error:', error)
  }
}

function updateDrones(data) {
  data.forEach((d, index) => {
    if (index < drones.length) {
      drones[index].x = d.x
      drones[index].y = d.y
      drones[index].z = d.z
      drones[index].battery = d.battery
    } else {
      drones.push({ ...d })
    }

    targetPositions[d.id] = { x: d.x, y: d.y, z: d.z }

    if (!droneMeshes[d.id]) {
      droneMeshes[d.id] = createDroneMesh(d)
    }
  })
}

function animate() {
  animationId = requestAnimationFrame(animate)

  Object.keys(droneMeshes).forEach((id) => {
    const mesh = droneMeshes[id]
    const target = targetPositions[id]

    if (target) {
      mesh.group.position.x += (target.x - mesh.group.position.x) * 0.05
      mesh.group.position.y += (target.y - mesh.group.position.y) * 0.05
      mesh.group.position.z += (target.z - mesh.group.position.z) * 0.05
    }

    mesh.ring.rotation.z += 0.02
    mesh.glow.scale.setScalar(1 + Math.sin(Date.now() * 0.003) * 0.1)

    if (selectedDrone.value === id) {
      mesh.group.scale.setScalar(1.3)
    } else {
      mesh.group.scale.setScalar(1)
    }
  })

  controls.update()
  renderer.render(scene, camera)
}

function handleResize() {
  if (!canvasContainer.value) return
  const width = canvasContainer.value.clientWidth
  const height = canvasContainer.value.clientHeight
  camera.aspect = width / height
  camera.updateProjectionMatrix()
  renderer.setSize(width, height)
}

onMounted(() => {
  initScene()
  connectWebSocket()
  window.addEventListener('resize', handleResize)
})

onUnmounted(() => {
  window.removeEventListener('resize', handleResize)
  if (animationId) cancelAnimationFrame(animationId)
  if (ws) ws.close()
  if (renderer) renderer.dispose()
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
}

.drone-battery {
  font-size: 14px;
  font-weight: 600;
  padding: 4px 10px;
  border-radius: 20px;
  background: rgba(82, 183, 136, 0.2);
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
  color: #ff6b6b;
  background: rgba(255, 107, 107, 0.15);
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
  background: linear-gradient(90deg, #e76f51, #ff6b6b);
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
