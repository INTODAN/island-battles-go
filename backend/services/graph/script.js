import * as echarts from 'https://cdn.jsdelivr.net/npm/echarts@5.5.1/dist/echarts.esm.min.js';

let currentGen = 0;

/** @type {number} */
const totalGens = window.totalGens;
/** @type {any[]} */
const generationsGraphJSON = window.generationsGraphJSON;

const initializedGens = { }; // gen_0 is initialized by go-echarts inline script

function initGen(index) {
    if (initializedGens[index]) return;

    const container = document.getElementById('gen_' + index);
    if (container) {
        const chart = echarts.init(container);
        chart.setOption(generationsGraphJSON[index]);
        initializedGens[index] = true;
    }
}

function showGen(index) {
    // Hide all
    for (let i = 0; i < totalGens; i++) {
        const el = document.getElementById('gen_' + i);
        if (el) el.style.display = 'none';
    }

    // Update labels
    const input = document.getElementById('gen-input');
    if (input) input.value = index + 1;

    const slider = document.getElementById('gen-slider');
    if (slider) slider.value = index;

    // Ensure target is initialized
    if (!initializedGens[index]) {
        initGen(index);
    }

    // Show current
    const current = document.getElementById('gen_' + index);
    if (current) {
        current.style.display = 'block';
        const chartInstance = echarts.getInstanceByDom(current);
        if (chartInstance) chartInstance.resize();
    }
}

async function backgroundInitialize() {
    // Initialize remaining charts one by one to keep the UI thread responsive
    for (let i = 1; i < totalGens; i++) {
        if (!initializedGens[i]) {
            // Small delay to yield to the browser's main thread
            await new Promise(resolve => setTimeout(resolve, 50));
            initGen(i);
        }
    }
}

function changeGen(delta) {
    let newGen = currentGen + delta;
    setGen(newGen);
}

function setGen(index) {
    if (index < 0) index = 0;
    if (index >= totalGens) index = totalGens - 1;
    currentGen = index;
    showGen(currentGen);
}

// Expose to window for global access
window.changeGen = changeGen;

function generatePlaceholders() {
    const container = document.createElement('div');
    container.id = 'charts-container';
    container.style.display = 'flex';
    container.style.justifyContent = 'center';
    document.body.appendChild(container);

    for (let i = 0; i < totalGens; i++) {
        const div = document.createElement('div');
        div.className = 'item chart-placeholder';
        div.id = 'gen_' + i;
        container.appendChild(div);
    }
}

// Init
document.addEventListener("DOMContentLoaded", function() {
    generatePlaceholders();

    const totalDisplay = document.getElementById('total-gens-display');
    if (totalDisplay) totalDisplay.innerText = totalGens;

    const slider = document.getElementById('gen-slider');
    if (slider) {
        slider.max = totalGens - 1;
        slider.addEventListener('input', function() {
            setGen(parseInt(this.value));
        });
    }

    const input = document.getElementById('gen-input');
    if (input) {
        input.max = totalGens;
        input.addEventListener('change', function() {
            let val = parseInt(this.value);
            if (isNaN(val)) val = 1;
            setGen(val - 1);
        });
    }

    showGen(0);

    const loading = document.getElementById('loading-overlay');
    if (loading) loading.style.display = 'none';

    // Start lazy background initialization
    backgroundInitialize().then();
});
