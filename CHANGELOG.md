# Changelog

## [0.26.1](https://github.com/gocov/gocov/compare/v0.26.0...v0.26.1) (2026-09-25)


### Bug Fixes

* list every part's files on the repo page, not only the last part's ([819fd33](https://github.com/gocov/gocov/commit/819fd3342e7ecffe20ef3c0ef62c2dedd74c39b0))

## [0.26.0](https://github.com/gocov/gocov/compare/v0.25.0...v0.26.0) (2026-09-25)


### Features

* give clover and simplecov their source extensions and file names ([#163](https://github.com/gocov/gocov/issues/163)) ([7cba000](https://github.com/gocov/gocov/commit/7cba00074f0cfba3eff5333e4d06784dcbc47004))
* rebuild the web UI as a single-page app, with a redesigned onboarding ([#146](https://github.com/gocov/gocov/issues/146)) ([2b770e8](https://github.com/gocov/gocov/commit/2b770e8f951ae72e1f246810b3f8476114967fb7))
* set the web UI in IBM Plex, and keep the design-sync inputs ([#151](https://github.com/gocov/gocov/issues/151)) ([65498c4](https://github.com/gocov/gocov/commit/65498c48f2ceb4e085998d2cc1f27b185088ed11))
* the repo page's upload history pages through its own endpoint ([#176](https://github.com/gocov/gocov/issues/176)) ([2cca661](https://github.com/gocov/gocov/commit/2cca661d2ce656417a99af6b8ebef58598d04c3f))


### Bug Fixes

* a fork PR named like the default branch is not its history ([#167](https://github.com/gocov/gocov/issues/167)) ([2f7950d](https://github.com/gocov/gocov/commit/2f7950db77eb7ae80e0447929eec659b1ccec663))
* bound coverage overlay work by block count, not declared line spans ([#141](https://github.com/gocov/gocov/issues/141)) ([54568fe](https://github.com/gocov/gocov/commit/54568fe3d7a4b076ed17f91a83f69fc29a741adc))
* explain the gate verdict against the baseline the gate judged ([#157](https://github.com/gocov/gocov/issues/157)) ([50385d1](https://github.com/gocov/gocov/commit/50385d1c9eb2abd9f654dbd6405bb779d7636851))
* keep the gate each upload was judged against ([#172](https://github.com/gocov/gocov/issues/172)) ([4de7a46](https://github.com/gocov/gocov/commit/4de7a46d53db7d1c425aab0d0fc92a12a5cb2a9d))
* keep upload and source pages out of search indexes ([#143](https://github.com/gocov/gocov/issues/143)) ([becfb62](https://github.com/gocov/gocov/commit/becfb62a27efe35c8f64a43dda077db7b828940e))
* OIDC uploads need a working forge connection on every forge ([#174](https://github.com/gocov/gocov/issues/174)) ([2b21600](https://github.com/gocov/gocov/commit/2b21600bf15e509bf7aebd29b50670b598e3f22b))
* one line rule for every view of a file's coverage ([#173](https://github.com/gocov/gocov/issues/173)) ([64553e9](https://github.com/gocov/gocov/commit/64553e95f3dc5376bed518d39cfaec3a2237f0ff))
* one rule for PR builds in a branch's history and its baseline ([#178](https://github.com/gocov/gocov/issues/178)) ([ab2099f](https://github.com/gocov/gocov/commit/ab2099f61856e5d6f3dfbac56382de8e9c3b4538))
* record the CI-wiring step again ([#145](https://github.com/gocov/gocov/issues/145)) ([c6db132](https://github.com/gocov/gocov/commit/c6db1329e970c798f2af733fcfeac5f7896caa9e))
* the CLI prints the server's upload warnings ([#179](https://github.com/gocov/gocov/issues/179)) ([2335d85](https://github.com/gocov/gocov/commit/2335d85d1aa7d75a53ee65d04dd0ed3a0e49c0c5))


### Performance Improvements

* latest reports through the lateral read; workspace repos by index ([#177](https://github.com/gocov/gocov/issues/177)) ([7f3cf7f](https://github.com/gocov/gocov/commit/7f3cf7fa633457f389bc69aacbb09b03c54790d0))
* pair diff paths through an index, not a scan per file ([#169](https://github.com/gocov/gocov/issues/169)) ([5779a19](https://github.com/gocov/gocov/commit/5779a1945c71ce093584e018b03845b4087ab48c))
* parallel repo-page reads, cached app shell, leaner settings check ([#175](https://github.com/gocov/gocov/issues/175)) ([0afdb65](https://github.com/gocov/gocov/commit/0afdb65e1d9be9ff0f7eb5d890225ab17e3fdcec))
* push forge surfaces and fetch the PR diff concurrently ([#156](https://github.com/gocov/gocov/issues/156)) ([62c41de](https://github.com/gocov/gocov/commit/62c41de82bee99c55cb23b0cbf10b910f403e3d3))
* read one file, not two whole uploads, for the source view ([#160](https://github.com/gocov/gocov/issues/160)) ([d961334](https://github.com/gocov/gocov/commit/d961334f42f431416ae4a8d61dc7dc4059a6f0d5))
* read only the viewer's repos and all dashboard previews at once ([#164](https://github.com/gocov/gocov/issues/164)) ([6147f2e](https://github.com/gocov/gocov/commit/6147f2e0f63f488d03eca4705baac5f92d38893b))
* read the dashboard's rows in one query, index uploads by repo ([#168](https://github.com/gocov/gocov/issues/168)) ([cae7f6a](https://github.com/gocov/gocov/commit/cae7f6a1478b81e9605777ad2204e1427afe5dad))
* report pages read what they need, side by side ([#180](https://github.com/gocov/gocov/issues/180)) ([88b522b](https://github.com/gocov/gocov/commit/88b522ba49d2dfe9c48d410b48adb157ffbaa530))
* skip file reads for single-part commits, batch them for many ([#159](https://github.com/gocov/gocov/issues/159)) ([829f018](https://github.com/gocov/gocov/commit/829f018f4a56150676b45f2b2d100cb682177bc4))

## [0.25.0](https://github.com/gocov/gocov/compare/v0.24.0...v0.25.0) (2026-09-08)


### ⚠ BREAKING CHANGES

* report, badge and settings URLs changed shape; README badges need the new form. No redirects from the old URLs.

### Features

* scope repo slugs and workspace prefixes per forge ([#137](https://github.com/gocov/gocov/issues/137)) ([55e5cc2](https://github.com/gocov/gocov/commit/55e5cc22e50496ce9ef85cc3daf78a3611181adc))
* the setup wizard leads with OIDC identity tokens instead of the upload token ([#134](https://github.com/gocov/gocov/issues/134)) ([9769d50](https://github.com/gocov/gocov/commit/9769d5081c6bbbe4c534a67c75d7a215ae4c9f55))


### Miscellaneous Chores

* release 0.25.0 ([#138](https://github.com/gocov/gocov/issues/138)) ([732f141](https://github.com/gocov/gocov/commit/732f141828e76b3d3d9fd035c5fef344a5ca2134))

## [0.24.0](https://github.com/gocov/gocov/compare/v0.23.0...v0.24.0) (2026-09-08)


### Features

* GitLab CI/CD Catalog component in the docs, the wizard and the release flow ([#131](https://github.com/gocov/gocov/issues/131)) ([aa0db9e](https://github.com/gocov/gocov/commit/aa0db9e956bc7ed7c263171264d85c2152ea1876))
* onboarding product events for the analytics snippet ([#125](https://github.com/gocov/gocov/issues/125)) ([099b8fc](https://github.com/gocov/gocov/commit/099b8fc16d028febc8233f942bc107bf32edc353))
* self-host on-ramp for the production compose file ([#130](https://github.com/gocov/gocov/issues/130)) ([9ed8579](https://github.com/gocov/gocov/commit/9ed857972dcf58e74c887ef8a33bc99f66b9fca1))


### Bug Fixes

* send click events over sendBeacon before the page navigates ([#127](https://github.com/gocov/gocov/issues/127)) ([cd907bf](https://github.com/gocov/gocov/commit/cd907bfdd0223afe2bffe71bb26b33b09c809a4e))
* stop the memory store aliasing user forge-workspace snapshots ([#128](https://github.com/gocov/gocov/issues/128)) ([d66ec65](https://github.com/gocov/gocov/commit/d66ec658e42cd9d684e75f5615c9d54af6bce8f5))

## [0.23.0](https://github.com/gocov/gocov/compare/v0.22.0...v0.23.0) (2026-09-06)


### Features

* page leaves, web vitals and setup-page session replay in the analytics snippet ([#123](https://github.com/gocov/gocov/issues/123)) ([e7d3a8c](https://github.com/gocov/gocov/commit/e7d3a8c30cd151242a095bc42189943fcfe9be87))

## [0.22.0](https://github.com/gocov/gocov/compare/v0.21.0...v0.22.0) (2026-09-06)


### Features

* opt-in PostHog analytics for the web UI ([#121](https://github.com/gocov/gocov/issues/121)) ([290032f](https://github.com/gocov/gocov/commit/290032f31ddb9153e2c7a6e31da917de407255ad))

## [0.21.0](https://github.com/gocov/gocov/compare/v0.20.0...v0.21.0) (2026-09-04)


### Features

* register repos on their first OIDC upload ([#116](https://github.com/gocov/gocov/issues/116)) ([922aebf](https://github.com/gocov/gocov/commit/922aebf1b40cafa396764205df8581b226a502b1))

## [0.20.0](https://github.com/gocov/gocov/compare/v0.19.0...v0.20.0) (2026-09-04)


### Features

* make workspace settings and tokens owner-only ([#113](https://github.com/gocov/gocov/issues/113)) ([9a68410](https://github.com/gocov/gocov/commit/9a684105ca06b2f513528f6bd04d03e42b63c6b2))

## [0.19.0](https://github.com/gocov/gocov/compare/v0.18.0...v0.19.0) (2026-09-04)


### Features

* record workspace roles from the forge ([#111](https://github.com/gocov/gocov/issues/111)) ([49a53e8](https://github.com/gocov/gocov/commit/49a53e850bd63d71919ec3d520b4767894fecae0))

## [0.18.0](https://github.com/gocov/gocov/compare/v0.17.0...v0.18.0) (2026-09-04)


### Features

* directory tree with filters and search for the files card ([#106](https://github.com/gocov/gocov/issues/106)) ([467aa50](https://github.com/gocov/gocov/commit/467aa503e73ea544b3dd336afc67b4023acd86ac))

## [0.17.0](https://github.com/gocov/gocov/compare/v0.16.0...v0.17.0) (2026-09-03)


### Features

* ignore patterns for files that should not count toward coverage ([#100](https://github.com/gocov/gocov/issues/100)) ([92f94dd](https://github.com/gocov/gocov/commit/92f94dd5db07e9a7e7e636498d6ccda310510faa))


### Miscellaneous Chores

* release 0.17.0 ([#102](https://github.com/gocov/gocov/issues/102)) ([2825caa](https://github.com/gocov/gocov/commit/2825caad1ce00f87441a27ec3c7de3c5568410f1))

## [0.16.0](https://github.com/gocov/gocov/compare/v0.15.0...v0.16.0) (2026-09-02)


### Miscellaneous Chores

* release 0.16.0 ([#95](https://github.com/gocov/gocov/issues/95)) ([4adb2b3](https://github.com/gocov/gocov/commit/4adb2b3d51a59f73a5604b117fc65cdc49e94afd))

## [0.15.0](https://github.com/gocov/gocov/compare/v0.14.0...v0.15.0) (2026-09-01)


### Miscellaneous Chores

* release 0.15.0 ([#91](https://github.com/gocov/gocov/issues/91)) ([9ef6c5a](https://github.com/gocov/gocov/commit/9ef6c5ab8b0140885921cd709372b088248258fd))

## [0.14.0](https://github.com/gocov/gocov/compare/v0.13.2...v0.14.0) (2026-08-29)


### Miscellaneous Chores

* release 0.14.0 ([#84](https://github.com/gocov/gocov/issues/84)) ([defe5bf](https://github.com/gocov/gocov/commit/defe5bf431ffe1eddeb942f1c0a55b456460de0e))

## [0.13.2](https://github.com/gocov/gocov/compare/v0.13.1...v0.13.2) (2026-08-29)


### Miscellaneous Chores

* release 0.13.2 ([#79](https://github.com/gocov/gocov/issues/79)) ([2821c28](https://github.com/gocov/gocov/commit/2821c28faa9efb321d738eaa871647e4f68b4b83))

## [0.13.1](https://github.com/gocov/gocov/compare/v0.13.0...v0.13.1) (2026-08-29)


### Miscellaneous Chores

* release 0.13.1 ([#75](https://github.com/gocov/gocov/issues/75)) ([36a70b4](https://github.com/gocov/gocov/commit/36a70b4f6a25a9ea771b4d5a9722c29517c35c00))
