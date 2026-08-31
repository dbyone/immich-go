<script lang="ts">
  import BottomInfo from '$lib/components/shared-components/side-bar/BottomInfo.svelte';
  import RecentAlbums from '$lib/components/shared-components/side-bar/RecentAlbums.svelte';
  import Sidebar from '$lib/components/sidebar/Sidebar.svelte';
  import { authManager } from '$lib/managers/auth-manager.svelte';
  import { featureFlagsManager } from '$lib/managers/feature-flags-manager.svelte';
  import { Route } from '$lib/route';
  import { recentAlbumsDropdown } from '$lib/stores/preferences.store';
  import { NavbarGroup, NavbarItem } from '@immich/ui';
  import {
    mdiAccount,
    mdiAccountMultiple,
    mdiAccountMultipleOutline,
    mdiFolderOutline,
    mdiImageAlbum,
    mdiImageMultiple,
    mdiImageMultipleOutline,
    mdiMagnify,
    mdiMap,
    mdiMapOutline,
    mdiTagMultipleOutline,
    mdiToolbox,
    mdiToolboxOutline,
    mdiTrashCan,
    mdiTrashCanOutline,
    mdiUploadOutline,
  } from '@mdi/js';
  import { t } from 'svelte-i18n';
  import { fly } from 'svelte/transition';
</script>

<Sidebar ariaLabel={$t('primary')}>
  <NavbarItem title={$t('photos')} href={Route.photos()} icon={mdiImageMultipleOutline} activeIcon={mdiImageMultiple} />

  {#if featureFlagsManager.value.search}
    <NavbarItem title={$t('explore')} href={Route.explore()} icon={mdiMagnify} />
  {/if}

  {#if featureFlagsManager.value.map}
    <NavbarItem title={$t('map')} href={Route.map()} icon={mdiMapOutline} activeIcon={mdiMap} />
  {/if}

  <!-- immich-go fork: people/places live inside Explore (MT-style nav);
       shared links are not implemented, so no dead entry. -->
  <NavbarItem
    title={$t('sharing')}
    href={Route.sharing()}
    icon={mdiAccountMultipleOutline}
    activeIcon={mdiAccountMultiple}
  />

  {#if authManager.preferences.folders.enabled && authManager.preferences.folders.sidebarWeb}
    <NavbarItem title={$t('folders')} href={Route.folders()} icon={{ icon: mdiFolderOutline, flipped: true }} />
  {/if}

  <NavbarGroup title={$t('library')} size="tiny" />

  <NavbarItem
    title={$t('albums')}
    href={Route.albums()}
    icon={{ icon: mdiImageAlbum, flipped: true }}
    bind:expanded={$recentAlbumsDropdown}
  >
    {#snippet items()}
      <span in:fly={{ y: -20 }} class="hidden md:block">
        <RecentAlbums />
      </span>
    {/snippet}
  </NavbarItem>

  {#if authManager.preferences.tags.enabled && authManager.preferences.tags.sidebarWeb}
    <NavbarItem title={$t('tags')} href={Route.tags()} icon={{ icon: mdiTagMultipleOutline, flipped: true }} />
  {/if}

  {#if authManager.preferences.recentlyAdded.sidebarWeb}
    <NavbarItem
      title={$t('recently_added')}
      href={Route.recentlyAdded()}
      icon={{ icon: mdiUploadOutline, flipped: true }}
    />
  {/if}

  <NavbarItem title={$t('utilities')} href={Route.utilities()} icon={mdiToolboxOutline} activeIcon={mdiToolbox} />

  {#if featureFlagsManager.value.trash}
    <NavbarItem title={$t('trash')} href={Route.trash()} icon={mdiTrashCanOutline} activeIcon={mdiTrashCan} />
  {/if}

  <BottomInfo />
</Sidebar>
