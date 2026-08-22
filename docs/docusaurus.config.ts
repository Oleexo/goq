import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'goq',
  tagline: 'LINQ for Go, built on generic methods',
  favicon: 'img/favicon.ico',

  url: 'https://oleexo.github.io',
  baseUrl: '/goq/',
  organizationName: 'oleexo',
  projectName: 'goq',
  trailingSlash: false,

  // A broken internal link fails the build. This is the site's link checker;
  // the pull_request job in docs.yml is what runs it.
  onBrokenLinks: 'throw',
  onBrokenAnchors: 'throw',
  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          routeBasePath: '/', // docs-only mode: the intro page IS the site root
          editUrl: 'https://github.com/oleexo/goq/tree/main/docs/',
        },
        blog: false,
        theme: {customCss: './src/css/custom.css'},
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    navbar: {
      title: 'goq',
      items: [
        {type: 'docSidebar', sidebarId: 'docs', position: 'left', label: 'Docs'},
        {href: 'https://pkg.go.dev/github.com/oleexo/goq', label: 'API reference', position: 'right'},
        {href: 'https://github.com/oleexo/goq', label: 'GitHub', position: 'right'},
      ],
    },
    footer: {style: 'dark', copyright: `Copyright © ${new Date().getFullYear()} Oleexo. MIT licensed.`},
    prism: {additionalLanguages: ['go']},
  } satisfies Preset.ThemeConfig,
};

export default config;
