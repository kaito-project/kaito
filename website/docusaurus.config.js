// @ts-check
// `@type` JSDoc annotations allow editor autocompletion and type checking
// (when paired with `@ts-check`).
// There are various equivalent ways to declare your Docusaurus config.
// See: https://docusaurus.io/docs/api/docusaurus-config

import { themes as prismThemes } from 'prism-react-renderer';

/** @type {import('@docusaurus/types').Config} */
const config = {
    title: 'KAITO',
    tagline: 'Kubernetes AI Toolchain Operator',
    favicon: 'img/favicon.ico',

    headTags: [
        {
            tagName: "meta",
            attributes: {
                // Allow Algolia crawler to index the site
                // See https://www.algolia.com/doc/tools/crawler/getting-started/create-crawler/#verify-your-domain.
                name: "algolia-site-verification",
                content: process.env.ALGOLIA_SITE_VERIFICATION || "DUMMY_SITE_VERIFICATION",
            }
        },
    ],

    // Set the production url of your site here
    url: 'https://kaito-project.github.io',
    // Set the /<baseUrl>/ pathname under which your site is served
    // For GitHub pages deployment, it is often '/<projectName>/'
    baseUrl: '/kaito/docs/',

    // GitHub pages deployment config.
    // If you aren't using GitHub pages, you don't need these.
    organizationName: 'kaito-project', // Usually your GitHub org/user name.
    projectName: 'kaito', // Usually your repo name.

    onBrokenLinks: 'throw',
    onBrokenMarkdownLinks: 'warn',

    // Even if you don't use internationalization, you can use this field to set
    // useful metadata like html lang. For example, if your site is Chinese, you
    // may want to replace "en" with "zh-Hans".
    i18n: {
        defaultLocale: 'en',
        locales: ['en'],
    },

    presets: [
        [
            'classic',
            /** @type {import('@docusaurus/preset-classic').Options} */
            ({
                docs: {
                    routeBasePath: '/',
                    sidebarPath: './sidebars.js',
                    // Please change this to your repo.
                    // Remove this to remove the "edit this page" links.
                    editUrl:
                        'https://github.com/kaito-project/kaito/tree/main/website/',
                },
                blog: false,
                theme: {
                    customCss: './src/css/custom.css',
                },
            }),
        ],
    ],

    themes: ['@docusaurus/theme-mermaid'],
    
    markdown: {
        mermaid: true,
    },

    themeConfig:
        /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
        ({
            // Replace with your project's social card
            image: 'img/kaito-logo.png',
            navbar: {
                title: 'KAITO',
                logo: {
                    alt: 'KAITO Logo',
                    src: 'img/kaito-logo.png',
                },
                items: [
                    {
                        type: 'docsVersionDropdown',
                        position: 'right',
                    },
                    {
                        href: 'https://github.com/kaito-project/kaito',
                        position: 'right',
                        className: 'header-github-link',
                        'aria-label': 'GitHub repository',
                    },
                ],
            },
            footer: {
                style: 'dark',
                links: [
                    {
                        title: 'Documentation',
                        items: [
                            {
                                label: 'Getting Started',
                                to: '/',
                            },
                            {
                                label: 'Installation',
                                to: '/installation',
                            },
                        ],
                    },
                    {
                        title: 'Community',
                        items: [
                            {
                                label: 'GitHub',
                                href: 'https://github.com/kaito-project/kaito',
                            },
                            {
                                label: 'Slack',
                                href: 'https://join.slack.com/t/kaito-z6a6575/shared_invite/zt-37gh89vw7-odHfqmPRc5oRnDG99SBJNA',
                            },
                        ],
                    },
                ],
                copyright: `Copyright © ${new Date().getFullYear()} KAITO Project, Built with Docusaurus.`,
            },
            prism: {
                theme: prismThemes.github,
                darkTheme: prismThemes.dracula,
                additionalLanguages: ['bash', 'json', 'yaml', 'go'],
            },
            colorMode: {
                defaultMode: 'light',
                disableSwitch: false,
                respectPrefersColorScheme: true,
            },
            announcementBar: {
                id: 'announcementBar-1', // Increment on change
                content: `⭐️ If you like KAITO, please give it a star on <a target="_blank" rel="noopener noreferrer" href="https://github.com/kaito-project/kaito">GitHub</a>!</a>`,
            },
            algolia: {
                appId: process.env.ALGOLIA_APP_ID || "DUMMY_APP_ID",
                apiKey: process.env.ALGOLIA_API_KEY || "DUMMY_API_KEY",
                indexName: 'KAITO',
                contextualSearch: true,
            }
        }),
};

export default config;
