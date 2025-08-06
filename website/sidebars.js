/**
 * Creating a sidebar enables you to:
 - create an ordered group of docs
 - render a sidebar for each doc of that group
 - provide next/previous navigation

 The sidebars can be generated from the filesystem, or explicitly defined here.

 Create as many sidebars as you want.
 */

// @ts-check

/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
    sidebar: [
        {
            type: 'category',
            label: 'Getting Started',
            collapsed: false,
            items: [
                'intro',
                'installation',
                'quick-start',
                'presets',
                'usage',
                'faq'
            ],
        },
        {
            type: 'category',
            label: 'Cloud Providers',
            collapsed: false,
            items: [
                'azure',
                'aws',
            ],
        },
        {
            type: 'category',
            label: 'Features',
            collapsed: false,
            items: [
                'inference',
                'multi-node-inference',
                'tuning',
                {
                    type: 'category',
                    label: 'Retrieval-Augmented Generation (RAG)',
                    link: {
                        type: 'doc',
                        id: 'rag',
                    },
                    items: [
                        'rag-api',
                    ],
                },
                'custom-model',
                'tool-calling',
                'model-as-oci-artifacts',
                'headlamp-kaito',
            ],
        },
        {
            type: 'category',
            label: 'Integrations',
            collapsed: false,
            items: [
                'aikit',
            ],
        },
        {
            type: 'category',
            label: 'Operations',
            collapsed: false,
            items: [
                'monitoring',
                'kaito-oom-prevention',
                'kaito-on-byo-gpu-nodes',
            ],
        },
        {
            type: 'category',
            label: 'Contributing',
            collapsed: false,
            items: [
                'contributing',
                'preset-onboarding',
                'proposals',
            ],
        },
    ],
};

export default sidebars;
