import * as echarts from 'https://cdn.jsdelivr.net/npm/echarts@5.5.1/dist/echarts.esm.min.js';

let currentGen = 0;

/**
 * @type {number}
 */
const totalGens = window.totalGens;

function showGen(index) {
    // Hide all
    for (let i = 0; i < totalGens; i++) {
        const el = document.getElementById('gen_' + i);
        if (el) el.style.display = 'none';
    }
    // Show current
    const current = document.getElementById('gen_' + index);
    if (current) current.style.display = 'block';

    // Update label
    document.getElementById('gen-display').innerText = 'Step ' + (index + 1) + ' / ' + totalGens;

    // Trigger resize to fix echarts rendering if hidden
    const chartInstance = echarts.getInstanceByDom(current);
    if (chartInstance) chartInstance.resize();
}

function changeGen(delta) {
    let newGen = currentGen + delta;
    if (newGen < 0) newGen = 0;
    if (newGen >= totalGens) newGen = totalGens - 1;
    currentGen = newGen;
    showGen(currentGen);
}

// Expose functions to window for HTML onclick handlers
window.changeGen = changeGen;
window.showGen = showGen;

// Init
document.addEventListener("DOMContentLoaded", function() {
    showGen(0);
});
