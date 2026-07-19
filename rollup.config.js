import deckyPlugin from '@decky/rollup';
import replace from '@rollup/plugin-replace';

const visualFixture = process.env.HV_QAM_VISUAL_FIXTURE ?? '';
const visualFixtures = new Set([
    '',
    'native-ready',
    'native-intel7',
    'z1-extreme',
    'z1-extreme-native',
    'hypervisor-ready',
    'setup-required',
    'recovery-required',
    'unsupported',
    'proton-missing',
    'proton-confirm',
    'proton-installing',
    'proton-success',
    'proton-failure'
]);

if (!visualFixtures.has(visualFixture)) {
    throw new Error(`Unknown HV_QAM_VISUAL_FIXTURE: ${visualFixture}`);
}

export default deckyPlugin({
    plugins: [
        replace({
            preventAssignment: true,
            values: { __HV_QAM_VISUAL_FIXTURE__: visualFixture }
        })
    ]
});
