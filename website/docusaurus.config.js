// @ts-check
// `@type` JSDoc annotations allow editor autocompletion and type checking
// (when paired with `@ts-check`).
// There are various equivalent ways to declare your Docusaurus config.
// See: https://docusaurus.io/docs/api/docusaurus-config

import { themes as prismThemes } from 'prism-react-renderer';

/** @type {import('@docusaurus/types').Config} */
const config = {
    title: 'Kaito',
    tagline: 'Kubernetes AI Toolchain Operator',
    favicon: 'img/favicon.ico',

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

    themeConfig:
        /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
        ({
            // Replace with your project's social card
            image: 'img/kaito-logo.png',
            navbar: {
                title: 'Kaito',
                logo: {
                    alt: 'Kaito Logo',
                    src: 'img/kaito-logo.png',
                },
                items: [
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
                                href: 'https://join.slack.com/t/kaito-z6a6575/shared_invite/zt-2wm17rttz-t4E6_rMIuY03DwBHaJq1sg',
                            },
                        ],
                    },
                ],
                copyright: `Copyright © ${new Date().getFullYear()} Kaito Project, Built with Docusaurus.`,
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
                content: `⭐️ If you like Kaito, please give it a star on <a target="_blank" rel="noopener noreferrer" href="https://github.com/kaito-project/kaito">GitHub</a>!</a>`,
            },
        }),
};

export default config;
